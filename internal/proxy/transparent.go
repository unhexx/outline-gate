package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Transparent is a TCP proxy for connections redirected via iptables/nft REDIRECT.
// It recovers the original destination with SO_ORIGINAL_DST and dials through Outline.
type Transparent struct {
	ListenAddr string
	Dialer     Dialer
	Logger     *slog.Logger
	Timeout    time.Duration

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

	orig, err := originalDST(conn)
	if err != nil {
		t.Logger.Debug("SO_ORIGINAL_DST failed", "err", err)
		return
	}
	t.Logger.Debug("transparent connect", "orig", orig)

	dctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	remote, err := t.Dialer.DialContext(dctx, "tcp", orig)
	if err != nil {
		t.Logger.Debug("transparent dial failed", "target", orig, "err", err)
		return
	}
	defer remote.Close()
	relay(conn, remote)
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
