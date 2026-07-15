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

	"github.com/unhex/outline-gate/internal/config"
	"github.com/unhex/outline-gate/internal/gateway"
	"github.com/unhex/outline-gate/internal/health"
	"github.com/unhex/outline-gate/internal/logging"
	"github.com/unhex/outline-gate/internal/outline"
	"github.com/unhex/outline-gate/internal/proxy"
	"github.com/unhex/outline-gate/internal/routing"
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

	// Initial connect (block briefly).
	cctx, ccancel := context.WithTimeout(ctx, 30*time.Second)
	err = client.Connect(cctx)
	ccancel()
	if err != nil {
		log.Warn("initial outline connect failed; will retry", "err", err)
	} else {
		log.Info("outline dialer ready", "server_ip", client.ServerIP())
	}

	var serverIPs []net.IP
	if ip := client.ServerIP(); ip != nil {
		serverIPs = []net.IP{ip}
	}
	engine := routing.New(cfg, serverIPs)

	var gw *gateway.Gateway
	if cfg.GatewayEnable {
		gw = gateway.New(cfg, engine, log)
	}

	hs := &health.Server{
		TunnelReady:     client.Ready,
		GatewayRequired: cfg.GatewayEnable,
		GatewayReady: func() bool {
			if gw == nil {
				return true
			}
			return gw.Active()
		},
	}
	hs.MarkStarted()

	httpSrv := &http.Server{
		Addr:              cfg.HealthListen,
		Handler:           hs.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 8)
	var wg sync.WaitGroup

	// Health
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("health listening", "addr", cfg.HealthListen)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("health: %w", err)
		}
	}()

	// Maintain outline readiness
	wg.Add(1)
	go func() {
		defer wg.Done()
		client.MaintainReady(ctx, func(ready bool) {
			log.Info("outline ready state", "ready", ready, "server_ip", client.ServerIP())
			if ready && gw != nil {
				// refresh server IP in engine
				var ips []net.IP
				if ip := client.ServerIP(); ip != nil {
					ips = []net.IP{ip}
				}
				_ = gw.UpdateEngine(routing.New(cfg, ips))
			}
		})
	}()

	// SOCKS5
	socks := &proxy.SOCKS5{
		ListenAddr: cfg.SOCKSListen,
		Dialer:     client,
		Logger:     log,
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := socks.ListenAndServe(ctx); err != nil {
			errCh <- fmt.Errorf("socks5: %w", err)
		}
	}()

	// Transparent proxy (for nft REDIRECT)
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

		// Apply gateway after a short delay so listeners are up
		wg.Add(1)
		go func() {
			defer wg.Done()
			// wait until tunnel ready or timeout, then apply rules
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
			if err := gw.Apply(); err != nil {
				log.Error("gateway apply failed", "err", err)
				errCh <- fmt.Errorf("gateway: %w", err)
			}
		}()
	}

	// SIGHUP reload lists from env is limited; re-load config files by re-reading env
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
				// preserve access key runtime; only lists/mode
				cfg = newCfg
				var ips []net.IP
				if ip := client.ServerIP(); ip != nil {
					ips = []net.IP{ip}
				}
				engine = routing.New(cfg, ips)
				if gw != nil {
					if err := gw.UpdateEngine(engine); err != nil {
						log.Error("gateway reload", "err", err)
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
		// continue to cleanup
		_ = err
	}

	// Teardown
	shctx, shcancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = httpSrv.Shutdown(shctx)
	shcancel()
	if gw != nil {
		_ = gw.Flush()
	}
	_ = socks.Close()
	cancel()
	wg.Wait()
	return nil
}
