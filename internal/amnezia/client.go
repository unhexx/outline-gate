package amnezia

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultIface   = "og-awg0"
	defaultUAPIDir = "/var/run/amneziawg"
	policyTable    = "51820"
)

// Client runs userspace amneziawg-go and dials TCP bound to that TUN.
// Kernel default route is never changed: L3/SOCKS keep using outline-gate
// redirect, and only tunnel dials go via BindToDevice.
type Client struct {
	Iface   string
	WGBin   string
	Conf    Config
	Logger  *slog.Logger
	Probe   string
	UAPIDir string

	mu       sync.Mutex
	cmd      *exec.Cmd
	ready    atomic.Bool
	dialer   *net.Dialer
	pinnedEP string
}

// Ready reports a live handshake (or successful probe).
func (c *Client) Ready() bool { return c.ready.Load() }

// EndpointIP is the AmneziaWG server address (must stay on the Direct path).
func (c *Client) EndpointIP() net.IP {
	if c == nil {
		return nil
	}
	return c.Conf.EndpointIP()
}

func (c *Client) log() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *Client) iface() string {
	if s := strings.TrimSpace(c.Iface); s != "" {
		return s
	}
	return defaultIface
}

func (c *Client) uapiPath() string {
	dir := c.UAPIDir
	if dir == "" {
		dir = defaultUAPIDir
	}
	return filepath.Join(dir, c.iface()+".sock")
}

func (c *Client) wgBin() (string, error) {
	if p := strings.TrimSpace(c.WGBin); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		if lp, err := exec.LookPath(p); err == nil {
			return lp, nil
		}
	}
	for _, cand := range []string{"amneziawg-go", "/usr/local/bin/amneziawg-go", "/opt/AmneziaVPN/bin/amneziawg-go"} {
		if lp, err := exec.LookPath(cand); err == nil {
			return lp, nil
		}
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("amneziawg-go not found (install in image or set AMNEZIA_WG_BIN)")
}

// Up starts AmneziaWG, configures UAPI, assigns the interface address, and
// waits for a handshake. It does not install a default route.
func (c *Client) Up(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.downLocked(); err != nil {
		c.log().Warn("amnezia previous down", "err", err)
	}
	bin, err := c.wgBin()
	if err != nil {
		return err
	}
	devBody, err := c.Conf.UAPIDevice()
	if err != nil {
		return err
	}
	peerBody, err := c.Conf.UAPIPeer()
	if err != nil {
		return err
	}
	ip, n, err := c.Conf.InterfaceAddr()
	if err != nil {
		return err
	}
	iface := c.iface()
	_ = exec.Command("ip", "link", "del", "dev", iface).Run()

	// HTTP handlers cancel when the client disconnects; the daemon must outlive that.
	upCtx, upCancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer upCancel()

	cmd := exec.Command(bin, "-f", iface)
	cmd.Env = append(os.Environ(),
		"WG_PROCESS_FOREGROUND=1",
		"LOG_LEVEL=verbose",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = &wgLogWriter{log: c.log(), stream: "stdout"}
	cmd.Stderr = &wgLogWriter{log: c.log(), stream: "stderr"}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("amneziawg-go start: %w", err)
	}
	c.cmd = cmd

	sock := c.uapiPath()
	deadline := time.Now().Add(8 * time.Second)
	for {
		if upCtx.Err() != nil {
			_ = c.downLocked()
			return upCtx.Err()
		}
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = c.downLocked()
			return fmt.Errorf("amneziawg-go uapi socket %s did not appear", sock)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Device obfuscation first (official Amnezia Linux client order), then peer.
	if err := uapiCmd(sock, "set=1\n"+devBody+"\n"); err != nil {
		_ = c.downLocked()
		return fmt.Errorf("amnezia uapi device: %w", err)
	}
	if err := uapiCmd(sock, "set=1\n"+peerBody+"\n"); err != nil {
		_ = c.downLocked()
		return fmt.Errorf("amnezia uapi peer: %w", err)
	}

	cidr := n.String()
	if ones, bits := n.Mask.Size(); bits == 32 && ones == 32 {
		cidr = ip.String() + "/32"
	}
	if err := runIP("addr", "replace", cidr, "dev", iface); err != nil {
		_ = c.downLocked()
		return err
	}
	_ = exec.Command("sysctl", "-w", "net.ipv6.conf."+iface+".disable_ipv6=1").Run()
	_ = runIP("link", "set", "dev", iface, "mtu", "1280")
	if err := runIP("link", "set", "dev", iface, "up"); err != nil {
		_ = c.downLocked()
		return err
	}
	if err := c.pinEndpointRoute(); err != nil {
		c.log().Warn("amnezia endpoint route", "err", err)
	}
	// Policy routing: traffic sourced from the TUN IP goes into AmneziaWG.
	// The main default route (and *.x.ai / GitHub bypass) stays untouched.
	_ = exec.Command("ip", "rule", "del", "from", ip.String(), "lookup", policyTable).Run()
	if err := runIP("route", "replace", "default", "dev", iface, "table", policyTable); err != nil {
		_ = c.downLocked()
		return err
	}
	if err := runIP("rule", "add", "from", ip.String(), "lookup", policyTable); err != nil {
		_ = c.downLocked()
		return err
	}

	c.dialer = &net.Dialer{
		Timeout:   15 * time.Second,
		LocalAddr: &net.TCPAddr{IP: ip},
		Control:   bindToDevice(iface),
	}

	hsDeadline := time.Now().Add(20 * time.Second)
	for {
		if upCtx.Err() != nil {
			_ = c.downLocked()
			return upCtx.Err()
		}
		if c.handshakeOK(sock) {
			break
		}
		if time.Now().After(hsDeadline) {
			_ = c.downLocked()
			return fmt.Errorf("amnezia handshake timeout with %s", c.Conf.Endpoint)
		}
		time.Sleep(400 * time.Millisecond)
	}

	if err := c.probeThrough(upCtx); err != nil {
		_ = c.downLocked()
		return err
	}

	c.ready.Store(true)
	c.log().Info("amneziawg up",
		"iface", iface,
		"endpoint", c.Conf.Endpoint,
		"addr", cidr,
	)
	return nil
}

// Down tears down the userspace daemon and TUN. Kernel routes are untouched.
func (c *Client) Down() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.downLocked()
}

func (c *Client) downLocked() error {
	c.ready.Store(false)
	c.dialer = nil
	if ip, _, err := c.Conf.InterfaceAddr(); err == nil && ip != nil {
		_ = exec.Command("ip", "rule", "del", "from", ip.String(), "lookup", policyTable).Run()
	}
	_ = exec.Command("ip", "route", "flush", "table", policyTable).Run()
	if c.pinnedEP != "" {
		_ = exec.Command("ip", "route", "del", c.pinnedEP).Run()
		c.pinnedEP = ""
	}
	var procErr error
	if c.cmd != nil && c.cmd.Process != nil {
		_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
			<-done
		case procErr = <-done:
		}
		c.cmd = nil
	}
	iface := c.iface()
	_ = exec.Command("ip", "link", "del", "dev", iface).Run()
	return procErr
}

// DialContext sends TCP through the AmneziaWG TUN (BindToDevice).
func (c *Client) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network %q (tcp only)", network)
	}
	if !c.ready.Load() {
		return nil, fmt.Errorf("amnezia dialer not ready")
	}
	c.mu.Lock()
	d := c.dialer
	c.mu.Unlock()
	if d == nil || !c.ready.Load() {
		return nil, fmt.Errorf("amnezia dialer not ready")
	}
	return d.DialContext(ctx, network, address)
}

func (c *Client) probeThrough(ctx context.Context) error {
	targets := make([]string, 0, 3)
	if p := strings.TrimSpace(c.Probe); p != "" {
		targets = append(targets, p)
	}
	for _, fb := range []string{"1.1.1.1:80", "8.8.8.8:53"} {
		dup := false
		for _, t := range targets {
			if t == fb {
				dup = true
				break
			}
		}
		if !dup {
			targets = append(targets, fb)
		}
	}
	var last error
	for _, addr := range targets {
		pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		conn, err := c.dialer.DialContext(pctx, "tcp", addr)
		cancel()
		if err == nil {
			_ = conn.Close()
			c.log().Info("amnezia probe ok", "target", addr)
			return nil
		}
		last = err
		c.log().Warn("amnezia probe failed", "target", addr, "err", err)
	}
	if last == nil {
		return nil
	}
	return fmt.Errorf("amnezia probe: %w", last)
}

func (c *Client) pinEndpointRoute() error {
	ep := c.Conf.EndpointIP()
	if ep == nil {
		return nil
	}
	out, err := exec.Command("ip", "-4", "route", "get", ep.String()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip route get %s: %w: %s", ep, err, strings.TrimSpace(string(out)))
	}
	fields := strings.Fields(string(out))
	via, dev := "", ""
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			via = fields[i+1]
		}
		if f == "dev" && i+1 < len(fields) {
			dev = fields[i+1]
		}
	}
	if dev == "" || dev == c.iface() {
		return fmt.Errorf("endpoint %s would route via %s", ep, dev)
	}
	args := []string{"route", "replace", ep.String()}
	if via != "" {
		args = append(args, "via", via)
	}
	args = append(args, "dev", dev)
	if err := runIP(args...); err != nil {
		return err
	}
	c.pinnedEP = ep.String()
	return nil
}

type wgLogWriter struct {
	log    *slog.Logger
	stream string
	buf    []byte
}

func (w *wgLogWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := strings.IndexByte(string(w.buf), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
		if line != "" && w.log != nil {
			w.log.Info("amneziawg-go", "stream", w.stream, "line", line)
		}
	}
	return len(p), nil
}

func (c *Client) handshakeOK(sock string) bool {
	out, err := uapiGet(sock)
	if err != nil {
		return false
	}
	var hs, rx int64
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch k {
		case "last_handshake_time_sec":
			hs = n
		case "rx_bytes":
			rx = n
		}
	}
	return hs > 0 && rx > 0
}

func bindToDevice(name string) func(network, address string, rc syscall.RawConn) error {
	return func(network, address string, rc syscall.RawConn) error {
		var sockErr error
		if err := rc.Control(func(fd uintptr) {
			sockErr = unix.BindToDevice(int(fd), name)
		}); err != nil {
			return err
		}
		return sockErr
	}
}

func runIP(args ...string) error {
	cmd := exec.Command("ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func uapiCmd(sock, payload string) error {
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, payload); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "errno=") {
			if line != "errno=0" {
				return fmt.Errorf("uapi %s", line)
			}
			return nil
		}
	}
}

func uapiGet(sock string) (string, error) {
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, "get=1\n\n"); err != nil {
		return "", err
	}
	var b strings.Builder
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return b.String(), err
		}
		if strings.TrimSpace(line) == "errno=0" {
			break
		}
		b.WriteString(line)
	}
	return b.String(), nil
}
