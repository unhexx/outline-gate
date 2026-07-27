package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Path values returned by PathDecider (match routing.Path semantics).
const (
	PathDirect = "direct"
	PathTunnel = "tunnel"
	PathDrop   = "drop"
)

// PathDecider chooses tunnel vs direct vs drop for an L3 destination IP.
type PathDecider interface {
	// DecidePath returns via ("tunnel"|"direct"|"drop") and optional rule label.
	DecidePath(dst net.IP) (via string, rule string)
}

// Transparent is a TCP proxy for connections redirected via iptables/nft REDIRECT.
// It recovers the original destination with SO_ORIGINAL_DST and dials through
// Outline (tunnel) or a direct dialer based on PathDecider.
type Transparent struct {
	ListenAddr string
	// Dialer is used for tunnelled connections (Outline).
	Dialer Dialer
	// DirectDialer is used for PathDirect; defaults to net.Dialer.
	DirectDialer Dialer
	// Decider selects tunnel/direct/drop. If nil, all traffic uses Dialer (tunnel).
	Decider PathDecider
	// ConnLog optionally records each connection for the live UI log.
	ConnLog ConnRecorder
	Logger  *slog.Logger
	Timeout time.Duration

	ln net.Listener
}

// ListenAndServe starts the transparent proxy.
func (t *Transparent) ListenAndServe(ctx context.Context) error {
	if t.Dialer == nil {
		return fmt.Errorf("transparent: dialer is required")
	}
	if t.Logger == nil {
		t.Logger = slog.Default()
	}
	if t.Timeout <= 0 {
		t.Timeout = 30 * time.Second
	}
	ln, err := net.Listen("tcp", t.ListenAddr)
	if err != nil {
		return err
	}
	t.ln = ln
	t.Logger.Info("transparent proxy listening", "addr", ln.Addr().String())

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.handle(ctx, conn)
		}()
	}
}

// Close stops the listener.
func (t *Transparent) Close() error {
	if t.ln != nil {
		return t.ln.Close()
	}
	return nil
}

func (t *Transparent) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	clientIP := clientIPOf(conn)
	orig, err := originalDST(conn)
	if err != nil {
		t.Logger.Debug("SO_ORIGINAL_DST failed", "err", err)
		return
	}
	t.Logger.Debug("transparent connect", "orig", orig)

	host, portStr, _ := net.SplitHostPort(orig)
	port := 0
	if p, err := strconv.Atoi(portStr); err == nil {
		port = p
	}

	via := PathTunnel
	rule := ""
	if t.Decider != nil {
		ip := net.ParseIP(host)
		via, rule = t.Decider.DecidePath(ip)
		if via == "" {
			via = PathTunnel
		}
	}

	if via == PathDrop {
		t.record(ConnEvent{
			Proto: "l3", ClientIP: clientIP, Target: orig, Host: host, Port: port,
			Via: PathDrop, Rule: rule, OK: false, Error: "dropped by routing policy",
		})
		return
	}

	dialer := t.Dialer
	if via == PathDirect {
		if t.DirectDialer != nil {
			dialer = t.DirectDialer
		} else {
			dialer = &net.Dialer{}
		}
	}

	dctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	start := time.Now()
	remote, err := dialer.DialContext(dctx, "tcp", orig)
	dur := time.Since(start).Milliseconds()
	if err != nil {
		t.Logger.Debug("transparent dial failed", "target", orig, "via", via, "err", err)
		t.record(ConnEvent{
			Proto: "l3", ClientIP: clientIP, Target: orig, Host: host, Port: port,
			Via: via, Rule: rule, OK: false, Error: err.Error(), DurationMs: dur,
		})
		return
	}
	t.Logger.Debug("transparent connect ok", "target", orig, "via", via)
	t.record(ConnEvent{
		Proto: "l3", ClientIP: clientIP, Target: orig, Host: host, Port: port,
		Via: via, Rule: rule, OK: true, DurationMs: dur,
	})
	defer remote.Close()
	relay(conn, remote)
}

func (t *Transparent) record(e ConnEvent) {
	if t.ConnLog != nil {
		t.ConnLog.RecordConnect(e)
	}
}

// originalDST recovers the pre-REDIRECT destination (IPv4).
func originalDST(conn net.Conn) (string, error) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return "", fmt.Errorf("not a TCP conn")
	}
	rc, err := tcp.SyscallConn()
	if err != nil {
		return "", err
	}
	const soOriginalDst = 80 // linux/include/uapi/linux/netfilter_ipv4.h
	var (
		addr syscall.RawSockaddrInet4
		cerr error
	)
	err = rc.Control(func(fd uintptr) {
		size := uint32(unsafe.Sizeof(addr))
		_, _, errno := syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			uintptr(syscall.IPPROTO_IP),
			uintptr(soOriginalDst),
			uintptr(unsafe.Pointer(&addr)),
			uintptr(unsafe.Pointer(&size)),
			0,
		)
		if errno != 0 {
			cerr = errno
		}
	})
	if err != nil {
		return "", err
	}
	if cerr != nil {
		return "", cerr
	}
	ip := net.IPv4(addr.Addr[0], addr.Addr[1], addr.Addr[2], addr.Addr[3])
	// sin_port is network byte order in the kernel structure.
	port := int(binary.BigEndian.Uint16((*[2]byte)(unsafe.Pointer(&addr.Port))[:]))
	return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port)), nil
}
