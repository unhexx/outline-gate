// Command outline-gate is an Outline VPN LAN gateway with split tunneling.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/unhexx/outline-gate/internal/amnezia"
	"github.com/unhexx/outline-gate/internal/bypass"
	"github.com/unhexx/outline-gate/internal/config"
	"github.com/unhexx/outline-gate/internal/connlog"
	"github.com/unhexx/outline-gate/internal/dnsdoh"
	"github.com/unhexx/outline-gate/internal/gateway"
	"github.com/unhexx/outline-gate/internal/health"
	"github.com/unhexx/outline-gate/internal/logging"
	"github.com/unhexx/outline-gate/internal/metrics"
	"github.com/unhexx/outline-gate/internal/netdns"
	"github.com/unhexx/outline-gate/internal/netrecover"
	"github.com/unhexx/outline-gate/internal/outline"
	"github.com/unhexx/outline-gate/internal/proxy"
	"github.com/unhexx/outline-gate/internal/routing"
	"github.com/unhexx/outline-gate/internal/tunnel"
	"github.com/unhexx/outline-gate/internal/version"
	"github.com/unhexx/outline-gate/internal/webui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "outline-gate: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log := logging.Setup(cfg.LogLevel, cfg.LogFormat)
	log.Info("starting outline-gate",
		"version", version.String(),
		"mode", cfg.RoutingMode,
		"gateway", cfg.GatewayEnable,
		"socks", cfg.SOCKSListen,
		"ui", cfg.UIEnable,
		"metrics", cfg.MetricsEnable,
		"access_key", config.RedactAccessKey(cfg.AccessKey),
	)
	if len(cfg.SOCKSAllowCIDRs) == 0 && !isLoopbackListen(cfg.SOCKSListen) {
		log.Warn("SOCKS5 has no authentication and SOCKS_ALLOW_CIDRS is empty; "+
			"any client that can reach the listener may use the proxy — restrict via firewall or set SOCKS_ALLOW_CIDRS",
			"listen", cfg.SOCKSListen,
		)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	expandedPersist := ""
	if p := strings.TrimSpace(cfg.AccessKeyPersistFile); p != "" {
		expandedPersist = p + ".expanded"
	}
	client, err := outline.New(outline.Options{
		AccessKey:       cfg.AccessKey,
		ReconnectBase:   cfg.ReconnectBase,
		ReconnectMax:    cfg.ReconnectMax,
		ProbeAddr:       cfg.ProbeAddr,
		ProbeInterval:   cfg.ProbeInterval,
		ProbeTimeout:    cfg.ProbeTimeout,
		ProbeFails:      cfg.ProbeFails,
		RefreshInterval: cfg.SSConfRefresh,
		Logger:          log,
		LastExpanded:    config.ReadPersistedKey(expandedPersist),
		OnExpanded: func(ss string) {
			if expandedPersist == "" {
				return
			}
			if err := config.PersistAccessKey(expandedPersist, ss); err != nil {
				log.Warn("persist expanded key", "err", err)
			}
		},
	})
	if err != nil {
		return err
	}
	client.OnUnready = func() {
		log.Warn("outline path stalled; clearing host conntrack", "server_ip", client.ServerIP())
		netrecover.ClearPath(client.ServerIP())
	}
	// Drop nft/conntrack left by a previous process. A container restart does
	// not clear the host network namespace.
	netrecover.FlushStaleNAT()
	netrecover.ClearPath(client.ServerIP())

	cctx, ccancel := context.WithTimeout(ctx, 30*time.Second)
	err = client.Connect(cctx)
	ccancel()
	if err != nil {
		log.Warn("initial outline connect failed; will retry", "err", err)
	} else {
		log.Info("outline dialer ready", "server_ip", client.ServerIP())
	}

	var awg *amnezia.Client
	if p := strings.TrimSpace(cfg.AmneziaConfigFile); p != "" {
		ac, err := amnezia.LoadFile(p)
		if err != nil {
			log.Warn("amnezia config not loaded", "path", p, "err", err)
		} else {
			awg = &amnezia.Client{
				Iface:  cfg.AmneziaIface,
				WGBin:  cfg.AmneziaWGBin,
				Conf:   ac,
				Logger: log,
				Probe:  cfg.ProbeAddr,
			}
			if uri := strings.TrimSpace(cfg.AmneziaVPNURI); uri != "" {
				if meta, _, uerr := amnezia.DecodeVPNURI(uri); uerr == nil {
					log.Info("amnezia vpn:// loaded", "name", meta.Name, "protocol", meta.ServiceProtocol)
				}
			}
			log.Info("amnezia backend available", "endpoint", ac.Endpoint, "iface", cfg.AmneziaIface)
		}
	}
	var amneziaDialer tunnel.Dialer
	if awg != nil {
		amneziaDialer = awg
	}
	dialMux := tunnel.NewMux(
		client, amneziaDialer,
		client.Ready,
		func() bool { return awg != nil && awg.Ready() },
		client.ServerIP,
		func() net.IP {
			if awg == nil {
				return nil
			}
			return awg.EndpointIP()
		},
	)

	var (
		mu          sync.Mutex
		gw          *gateway.Gateway
		engine      *routing.Engine
		bypassMgr   *bypass.Manager
		rebuildPush func()
	)

	// getCfg returns the current config under mu (safe vs SIGHUP reload).
	getCfg := func() *config.Config {
		mu.Lock()
		defer mu.Unlock()
		return cfg
	}

	rebuildPush = func() {
		mu.Lock()
		defer mu.Unlock()
		if bypassMgr == nil {
			return
		}
		engine = routing.NewWithBypass(cfg, bypassMgr.EffectiveBypassNets(), tunnelBypassIPs(client, awg))
		if gw != nil {
			if err := gw.UpdateEngine(engine); err != nil {
				log.Error("gateway update after bypass change", "err", err)
			}
		}
	}

	bypassMgr = bypass.NewManager(bypass.Options{
		Store:        bypass.NewStore(cfg.BypassRulesFile),
		StaticBypass: cfg.BypassCIDRs,
		LookupIP:     netdns.LookupIP,
		Logger:       log,
		RefreshEvery: cfg.BypassDNSRefresh,
		OnChange:     rebuildPush,
	})

	if err := bypassMgr.Load(ctx); err != nil {
		log.Warn("bypass rules load", "err", err)
	}

	blockMgr := bypass.NewManager(bypass.Options{
		Store:        bypass.NewStore(cfg.BlockRulesFile),
		LookupIP:     netdns.LookupIP,
		Logger:       log,
		RefreshEvery: cfg.BypassDNSRefresh,
	})
	if err := blockMgr.Load(ctx); err != nil {
		log.Warn("block rules load", "err", err)
	}

	mu.Lock()
	engine = routing.NewWithBypass(cfg, bypassMgr.EffectiveBypassNets(), tunnelBypassIPs(client, awg))
	if cfg.GatewayEnable {
		gw = gateway.New(cfg, engine, log)
	}
	mu.Unlock()

	hs := &health.Server{
		TunnelReady:     dialMux.Ready,
		GatewayRequired: cfg.GatewayEnable,
		GatewayReady: func() bool {
			mu.Lock()
			defer mu.Unlock()
			if gw == nil {
				return true
			}
			return gw.Active()
		},
	}
	hs.MarkStarted()

	connStore := connlog.New(500)
	var met *metrics.Registry
	if cfg.MetricsEnable {
		met = metrics.New()
	}
	if met != nil {
		client.OnProbe = func(ok bool) { met.ObserveProbe(ok) }
		client.OnRefresh = func(err error, changed bool) { met.ObserveRefresh(err == nil, changed) }
	}
	if cfg.ProbeAddr != "" || cfg.SSConfRefresh > 0 {
		log.Info("outline supervisor",
			"probe", cfg.ProbeAddr,
			"probe_interval", cfg.ProbeInterval,
			"probe_fails", cfg.ProbeFails,
			"ssconf_refresh", cfg.SSConfRefresh,
		)
	}
	connHook := proxy.NewStoreHook(func(e proxy.ConnEvent) {
		connStore.FromFields(e.Proto, e.ClientIP, e.Target, e.Host, e.Port, e.Via, e.Rule, e.OK, e.Error, e.DurationMs)
		if met != nil {
			met.ObserveConnection(e.Proto, e.Via, e.OK)
		}
	})

	httpMux := hs.Mux()
	if met != nil {
		httpMux.Handle("/metrics", met.Handler())
		log.Info("prometheus metrics enabled", "path", "/metrics")
	}
	tunnelStatus := func() webui.TunnelStatus {
		st := webui.TunnelStatus{
			Backend: dialMux.Backend(),
			Ready:   dialMux.Ready(),
			Outline: webui.OutlineStatus{
				AccessKeyRedacted: config.RedactAccessKey(client.AccessKey()),
				Ready:             client.Ready(),
				PersistFile:       cfg.AccessKeyPersistFile,
			},
			Amnezia: webui.AmneziaStatus{
				Configured: awg != nil,
				Label:      "Швеция",
				Iface:      cfg.AmneziaIface,
			},
		}
		if ip := client.ServerIP(); ip != nil {
			st.Outline.ServerIP = ip.String()
		}
		if awg != nil {
			st.Amnezia.Ready = awg.Ready()
			st.Amnezia.Endpoint = awg.Conf.Endpoint
			if awg.Iface != "" {
				st.Amnezia.Iface = awg.Iface
			}
		}
		return st
	}
	switchBackend := func(ctx context.Context, name string) (webui.TunnelStatus, error) {
		name = tunnel.Normalize(name)
		switch name {
		case tunnel.BackendAmnezia:
			if awg == nil {
				return tunnelStatus(), fmt.Errorf("нет конфига AmneziaWG (%s)", cfg.AmneziaConfigFile)
			}
			if err := awg.Up(ctx); err != nil {
				_ = dialMux.SetBackend(tunnel.BackendOutline)
				return tunnelStatus(), err
			}
			if err := dialMux.SetBackend(tunnel.BackendAmnezia); err != nil {
				_ = awg.Down()
				return tunnelStatus(), err
			}
		case tunnel.BackendOutline:
			_ = dialMux.SetBackend(tunnel.BackendOutline)
			if awg != nil {
				_ = awg.Down()
			}
		default:
			return tunnelStatus(), fmt.Errorf("unknown tunnel backend %q", name)
		}
		if err := config.PersistBackend(cfg.BackendPersistFile, dialMux.Backend()); err != nil {
			log.Warn("persist tunnel backend", "err", err)
		}
		rebuildPush()
		netrecover.ClearPath(dialMux.ServerIP())
		log.Info("tunnel backend", "backend", dialMux.Backend(), "ready", dialMux.Ready(), "server_ip", dialMux.ServerIP())
		st := tunnelStatus()
		st.Message = "активен " + dialMux.Backend()
		return st, nil
	}

	if cfg.TunnelBackend == tunnel.BackendAmnezia {
		sctx, scancel := context.WithTimeout(ctx, 45*time.Second)
		_, err := switchBackend(sctx, tunnel.BackendAmnezia)
		scancel()
		if err != nil {
			log.Warn("amnezia startup failed; staying on outline", "err", err)
		}
	}

	if cfg.UIEnable {
		ui := &webui.Server{
			Manager: bypassMgr,
			Block:   blockMgr,
			Outline: &webui.ClientOutline{
				Ready:     client.Ready,
				ServerIP:  client.ServerIP,
				AccessKey: client.AccessKey,
				SetKey:    client.SetAccessKey,
				OnReplaced: func() {
					log.Info("outline key replaced via UI",
						"access_key", config.RedactAccessKey(client.AccessKey()),
						"server_ip", client.ServerIP(),
					)
					rebuildPush()
				},
				PersistPath: cfg.AccessKeyPersistFile,
			},
			Tunnel:  &uiTunnel{status: tunnelStatus, set: switchBackend},
			ConnLog: connStore,
			Status: func() webui.RuntimeStatus {
				c := getCfg()
				return webui.RuntimeStatus{
					Version:       version.String(),
					SOCKSListen:   c.SOCKSListen,
					GatewayEnable: c.GatewayEnable,
					HealthListen:  c.HealthListen,
				}
			},
			Version: version.String(),
			Token:   cfg.UIToken,
			Static:  webui.StaticFS(),
			RestartServices: func(rctx context.Context) (string, error) {
				if dialMux.Backend() == tunnel.BackendAmnezia && awg != nil {
					netrecover.ClearPath(awg.EndpointIP())
					if err := awg.Up(rctx); err != nil {
						return "", err
					}
					rebuildPush()
					return "AmneziaWG переподключён", nil
				}
				netrecover.ClearPath(client.ServerIP())
				if err := client.Reconnect(rctx); err != nil {
					if fresh, lerr := config.Load(); lerr == nil && fresh.AccessKey != client.AccessKey() {
						if serr := client.SetAccessKey(rctx, fresh.AccessKey); serr == nil {
							err = nil
						}
					}
					if err != nil {
						return "", err
					}
				}
				mu.Lock()
				g := gw
				mu.Unlock()
				if g != nil {
					if gerr := g.Apply(); gerr != nil {
						log.Error("gateway reapply after service restart", "err", gerr)
						return "туннель поднят, nft не применился: " + gerr.Error(), nil
					}
				}
				log.Info("services restarted from UI", "server_ip", client.ServerIP(), "ready", client.Ready())
				return "службы перезапущены", nil
			},
			RestartContainer: func() {
				log.Info("container restart requested from UI")
				cancel()
			},
		}
		ui.Mount(httpMux)
		log.Info("management UI enabled", "path", "/ui/", "key_persist", cfg.AccessKeyPersistFile)
	}

	httpSrv := &http.Server{
		Addr:              cfg.HealthListen,
		Handler:           httpMux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 8)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("health listening", "addr", cfg.HealthListen)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("health: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		bypassMgr.RunRefreshLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		blockMgr.RunRefreshLoop(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errCh <- fmt.Errorf("outline maintain: panic: %v", r)
			}
		}()
		client.MaintainReady(ctx, func(ready bool) {
			log.Info("outline ready state", "ready", ready, "server_ip", client.ServerIP())
			if ready {
				rebuildPush()
			}
		})
	}()

	if cfg.DNSEnabled() {
		doh := &dnsdoh.Server{
			Listen:  cfg.DNSListen,
			URL:     cfg.DoHURL,
			Addr:    cfg.DoHAddr,
			Dialer:  dialMux,
			Logger:  log,
			Timeout: 8 * time.Second,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := doh.ListenAndServe(ctx); err != nil {
				errCh <- fmt.Errorf("dns-over-https: %w", err)
			}
		}()
	}

	directDial := netdns.Dialer()
	socks := &proxy.SOCKS5{
		ListenAddr:   cfg.SOCKSListen,
		Dialer:       dialMux,
		DirectDialer: directDial,
		Bypass:       bypassMgr,
		Block:        blockMgr,
		AllowCIDRs:   cfg.SOCKSAllowCIDRs,
		ConnLog:      connHook,
		Logger:       log,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := socks.ListenAndServe(ctx); err != nil {
			errCh <- fmt.Errorf("socks5: %w", err)
		}
	}()

	if cfg.GatewayEnable {
		tp := &proxy.Transparent{
			ListenAddr:   cfg.TransproxyListen,
			Dialer:       dialMux,
			DirectDialer: directDial,
			Decider: &proxy.EnginePathDecider{
				Mu:     &mu,
				Engine: func() *routing.Engine { return engine },
				Bypass: bypassMgr,
				Block:  blockMgr,
			},
			ConnLog: connHook,
			Logger:  log,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tp.ListenAndServe(ctx); err != nil {
				errCh <- fmt.Errorf("transparent: %w", err)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Wait until tunnel is ready or 60s elapses (then apply anyway).
			deadline := time.Now().Add(60 * time.Second)
			for !dialMux.Ready() {
				if ctx.Err() != nil {
					return
				}
				if time.Now().After(deadline) {
					log.Warn("applying gateway rules without tunnel ready")
					break
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(500 * time.Millisecond):
				}
			}
			if ctx.Err() != nil {
				return
			}
			rebuildPush()

			c := getCfg()
			base := c.ReconnectBase
			maxDelay := c.ReconnectMax
			if base <= 0 {
				base = time.Second
			}
			if maxDelay < base {
				maxDelay = 60 * time.Second
			}
			delay := base
			for attempt := 1; ; attempt++ {
				if ctx.Err() != nil {
					return
				}
				mu.Lock()
				g := gw
				mu.Unlock()
				if g == nil {
					return
				}
				if err := g.Apply(); err == nil {
					return
				} else {
					log.Error("gateway apply failed; will retry",
						"err", err, "attempt", attempt, "next_in", delay)
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
			}
		}()
	}

	sigHUP := make(chan os.Signal, 1)
	signal.Notify(sigHUP, syscall.SIGHUP)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigHUP:
				log.Info("SIGHUP: reloading config")
				newCfg, err := config.Load()
				if err != nil {
					log.Error("reload failed", "err", err)
					continue
				}
				mu.Lock()
				cfg = newCfg
				socks.SetAllowCIDRs(cfg.SOCKSAllowCIDRs)
				if gw != nil {
					engine = routing.NewWithBypass(cfg, bypassMgr.EffectiveBypassNets(), tunnelBypassIPs(client, awg))
					wasActive := gw.Active()
					gw = gateway.New(cfg, engine, log)
					mu.Unlock()
					bypassMgr.SetStatic(cfg.BypassCIDRs)
					if err := bypassMgr.Load(ctx); err != nil {
						log.Error("bypass reload", "err", err)
					}
					if err := blockMgr.Load(ctx); err != nil {
						log.Error("block reload", "err", err)
					}
					// Load triggers OnChange → rebuildPush, which refreshes engine + gateway
					if wasActive {
						if err := gw.Apply(); err != nil {
							log.Error("gateway reload", "err", err)
						}
					}
				} else {
					mu.Unlock()
					bypassMgr.SetStatic(cfg.BypassCIDRs)
					if err := bypassMgr.Load(ctx); err != nil {
						log.Error("bypass reload", "err", err)
					}
					if err := blockMgr.Load(ctx); err != nil {
						log.Error("block reload", "err", err)
					}
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errCh:
		log.Error("fatal", "err", err)
		cancel()
		_ = err
	}

	shctx, shcancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = httpSrv.Shutdown(shctx)
	shcancel()
	mu.Lock()
	g := gw
	mu.Unlock()
	if g != nil {
		if err := g.Flush(); err != nil {
			log.Error("gateway flush on shutdown", "err", err)
		}
	}
	_ = socks.Close()
	if awg != nil {
		if err := awg.Down(); err != nil {
			log.Warn("amnezia down", "err", err)
		}
	}
	cancel()

	// Bounded wait: Accept() loops exit on closed listeners; stuck handlers
	// must not block process exit forever.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		log.Warn("goroutines did not exit in time; continuing shutdown")
	}
	return nil
}

func serverIPsFrom(client *outline.Client) []net.IP {
	if client == nil {
		return nil
	}
	if ip := client.ServerIP(); ip != nil {
		return []net.IP{ip}
	}
	return nil
}

func tunnelBypassIPs(client *outline.Client, awg *amnezia.Client) []net.IP {
	ips := serverIPsFrom(client)
	if awg != nil {
		if ip := awg.EndpointIP(); ip != nil {
			ips = append(ips, ip)
		}
	}
	return ips
}

type uiTunnel struct {
	status func() webui.TunnelStatus
	set    func(context.Context, string) (webui.TunnelStatus, error)
}

func (t *uiTunnel) Status() webui.TunnelStatus {
	if t == nil || t.status == nil {
		return webui.TunnelStatus{}
	}
	return t.status()
}

func (t *uiTunnel) SetBackend(ctx context.Context, backend string) (webui.TunnelStatus, error) {
	if t == nil || t.set == nil {
		return webui.TunnelStatus{}, fmt.Errorf("tunnel switcher not configured")
	}
	return t.set(ctx, backend)
}

// isLoopbackListen reports whether addr binds only to loopback (127.0.0.1 / ::1).
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// hostnames like "localhost"
		return strings.EqualFold(host, "localhost")
	}
	return ip.IsLoopback()
}
