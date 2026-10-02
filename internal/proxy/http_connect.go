package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *SOCKS5) handleHTTP(ctx context.Context, conn net.Conn, clientIP string, timeout time.Duration) {
	log := s.Logger
	br, ok := conn.(*prefixConn)
	var r *bufio.Reader
	if ok {
		r = br.r
	} else {
		r = bufio.NewReader(conn)
	}
	req, err := http.ReadRequest(r)
	if err != nil {
		return
	}
	if req.Method != http.MethodConnect {
		_, _ = io.WriteString(conn, "HTTP/1.1 405 Method Not Allowed\r\nConnection: close\r\n\r\n")
		return
	}
	hostport := req.Host
	if hostport == "" {
		hostport = req.URL.Host
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.TrimSpace(hostport)
		portStr = "443"
	}
	portInt, err := strconv.Atoi(portStr)
	if err != nil || portInt <= 0 {
		_, _ = io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")
		return
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			host = v4.String()
		}
	}
	target := net.JoinHostPort(host, strconv.Itoa(portInt))

	if blocked, rule := matchChecker(s.Block, host); blocked {
		s.record(ConnEvent{
			Proto: "http", ClientIP: clientIP, Target: target, Host: host, Port: portInt,
			Via: "drop", Rule: rule, OK: false, Error: "blocked",
		})
		_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n")
		return
	}

	remote, via, rule, dur, err := s.dialTarget(ctx, host, target, timeout)
	if err != nil {
		log.Warn("HTTP CONNECT dial failed", "target", target, "via", via, "err", err)
		s.record(ConnEvent{
			Proto: "http", ClientIP: clientIP, Target: target, Host: host, Port: portInt,
			Via: via, Rule: rule, OK: false, Error: err.Error(), DurationMs: dur,
		})
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n")
		return
	}
	s.record(ConnEvent{
		Proto: "http", ClientIP: clientIP, Target: target, Host: host, Port: portInt,
		Via: via, Rule: rule, OK: true, DurationMs: dur,
	})
	defer remote.Close()
	if _, err := fmt.Fprintf(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	relay(conn, remote)
}
