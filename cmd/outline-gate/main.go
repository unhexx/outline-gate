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
	"sync"
	"syscall"
	"time"

	"github.com/unhex/outline-gate/internal/bypass"
	"github.com/unhex/outline-gate/internal/config"
	"github.com/unhex/outline-gate/internal/gateway"
	"github.com/unhex/outline-gate/internal/health"
	"github.com/unhex/outline-gate/internal/logging"
	"github.com/unhex/outline-gate/internal/outline"
	"github.com/unhex/outline-gate/internal/proxy"
	"github.com/unhex/outline-gate/internal/routing"
	"github.com/unhex/outline-gate/internal/webui"
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
		"mode", cfg.RoutingMode,
		"gateway", cfg.GatewayEnable,
		"socks", cfg.SOCKSListen,
		"ui", cfg.UIEnable,
		"access_key", config.RedactAccessKey(cfg.AccessKey),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client, err := outline.New(outline.Options{
		AccessKey:     cfg.AccessKey,
		ReconnectBase: cfg.ReconnectBase,
		ReconnectMax:  cfg.ReconnectMax,
	})
	if err != nil {
		return err
	}

	cctx, ccancel := context.WithTimeout(ctx, 30*time.Second)
	err = client.Connect(cctx)
	ccancel()
	if err != nil {
		log.Warn("initial outline connect failed; will retry", "err", err)
	} else {
		log.Info("outline dialer ready", "server_ip", client.ServerIP())
	}

	var (
		mu         sync.Mutex
		gw         *gateway.Gateway
		bypassMgr  *bypass.Manager
		rebuildPush func()
	)

	rebuildPush = func() {
		mu.Lock()
		defer mu.Unlock()
		if bypassMgr == nil {
			return
		}
		eng := routing.NewWithBypass(cfg, bypassMgr.EffectiveBypassNets(), serverIPsFrom(client))
		if gw != nil {
			if err := gw.UpdateEngine(eng); err != nil {
				log.Error("gateway update after bypass change", "err", err)
			}
		}
	}

	bypassMgr = bypass.NewManager(bypass.Options{
		Store:        bypass.NewStore(cfg.BypassRulesFile),
		StaticBypass: cfg.BypassCIDRs,
		Logger:       log,
		RefreshEvery: cfg.BypassDNSRefresh,
		OnChange:     rebuildPush,
	})

	if err := bypassMgr.Load(ctx); err != nil {
		log.Warn("bypass rules load", "err", err)
	}

	mu.Lock()
	engine := routing.NewWithBypass(cfg, bypassMgr.EffectiveBypassNets(), serverIPsFrom(client))
	if cfg.GatewayEnable {
		gw = gateway.New(cfg, engine, log)
	}
	mu.Unlock()

	hs := &health.Server{
		TunnelReady:     client.Ready,
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

	mux := hs.Mux()
	if cfg.UIEnable {
		ui := &webui.Server{
			Manager: bypassMgr,
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
			Token:  cfg.UIToken,
			Static: webui.StaticFS(),
		}
		ui.Mount(mux)
		log.Info("management UI enabled", "path", "/ui/", "key_persist", cfg.AccessKeyPersistFile)
	}

	httpSrv := &http.Server{
		Addr:              cfg.HealthListen,
		Handler:           mux,
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
		client.MaintainReady(ctx, func(ready bool) {
			log.Info("outline ready state", "ready", ready, "server_ip", client.ServerIP())
			if ready {
				rebuildPush()
			}
		})
	}()

	socks := &proxy.SOCKS5{
		ListenAddr: cfg.SOCKSListen,
		Dialer:     client,
		Bypass:     bypassMgr,
		Logger:     log,
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
			ListenAddr: cfg.TransproxyListen,
			Dialer:     client,
			Logger:     log,
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
			deadline := time.After(60 * time.Second)
			for {
				if client.Ready() {
					break
				}
				select {
				case <-ctx.Done():
					return
				case <-deadline:
					log.Warn("applying gateway rules without tunnel ready")
					goto apply
				case <-time.After(500 * time.Millisecond):
				}
			}
		apply:
			rebuildPush()
			mu.Lock()
			g := gw
			mu.Unlock()
			if g == nil {
				return
			}
			if err := g.Apply(); err != nil {
				log.Error("gateway apply failed", "err", err)
				errCh <- fmt.Errorf("gateway: %w", err)
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
				if gw != nil {
					eng := routing.NewWithBypass(cfg, bypassMgr.EffectiveBypassNets(), serverIPsFrom(client))
					wasActive := gw.Active()
					gw = gateway.New(cfg, eng, log)
					mu.Unlock()
					bypassMgr.SetStatic(cfg.BypassCIDRs)
					if err := bypassMgr.Load(ctx); err != nil {
						log.Error("bypass reload", "err", err)
					}
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
		_ = g.Flush()
	}
	_ = socks.Close()
	cancel()
	wg.Wait()
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
