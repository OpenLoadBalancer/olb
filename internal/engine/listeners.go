package engine

import (
	"context"
	"fmt"

	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/listener"
	"github.com/openloadbalancer/olb/internal/logging"
	"github.com/openloadbalancer/olb/internal/proxy/l4"
	"github.com/openloadbalancer/olb/internal/security"
)

// startListeners creates and starts all listeners.
// Supports HTTP, HTTPS (with optional mTLS), TCP (L4), and UDP (L4) protocols.
// If a listener fails to start, previously started listeners are stopped.
func (e *Engine) startListeners() error {
	started := 0
	for _, listenerCfg := range e.config.Listeners {
		switch listenerCfg.Protocol {
		case "tcp":
			if err := e.startTCPListener(listenerCfg); err != nil {
				e.stopListeners(started)
				return fmt.Errorf("failed to start TCP listener %s: %w", listenerCfg.Name, err)
			}

		case "udp":
			if err := e.startUDPListener(listenerCfg); err != nil {
				e.stopListeners(started)
				return fmt.Errorf("failed to start UDP listener %s: %w", listenerCfg.Name, err)
			}

		default:
			if err := e.startHTTPListener(listenerCfg); err != nil {
				e.stopListeners(started)
				return fmt.Errorf("failed to start listener %s: %w", listenerCfg.Name, err)
			}
		}
		started++
	}

	return nil
}

// stopListeners stops the first n listeners that were started.
func (e *Engine) stopListeners(n int) {
	ctx, cancel := context.WithTimeout(context.Background(), e.listenerStopTimeout)
	defer cancel()
	for i := 0; i < n && i < len(e.listeners); i++ {
		e.listeners[i].Stop(ctx)
	}
	e.listeners = e.listeners[:0]
}

// startTCPListener creates and starts a TCP (L4) proxy listener.
func (e *Engine) startTCPListener(listenerCfg *config.Listener) error {
	// Resolve the backend pool for this TCP listener
	poolName := listenerCfg.Pool
	if poolName == "" && len(listenerCfg.Routes) > 0 {
		poolName = listenerCfg.Routes[0].Pool
	}

	pool := e.poolManager.GetPool(poolName)
	if pool == nil {
		return fmt.Errorf("backend pool %q not found for TCP listener %s", poolName, listenerCfg.Name)
	}

	// Create TCP proxy with simple round-robin balancer
	tcpBalancer := l4.NewSimpleBalancer()
	tcpProxy := l4.NewTCPProxy(pool, tcpBalancer, l4.DefaultTCPProxyConfig())

	// Create TCP listener
	tcpListener, err := l4.NewTCPListener(&l4.TCPListenerOptions{
		Name:    listenerCfg.Name,
		Address: listenerCfg.Address,
		Proxy:   tcpProxy,
	})
	if err != nil {
		return err
	}

	if err := tcpListener.Start(); err != nil {
		return err
	}

	e.listeners = append(e.listeners, tcpListener)

	e.logger.Info("TCP listener started",
		logging.String("name", listenerCfg.Name),
		logging.String("address", tcpListener.Address()),
		logging.String("pool", poolName),
	)

	return nil
}

// startUDPListener creates and starts a UDP (L4) proxy.
func (e *Engine) startUDPListener(listenerCfg *config.Listener) error {
	// Resolve the backend pool for this UDP listener
	poolName := listenerCfg.Pool
	if poolName == "" && len(listenerCfg.Routes) > 0 {
		poolName = listenerCfg.Routes[0].Pool
	}

	pool := e.poolManager.GetPool(poolName)
	if pool == nil {
		return fmt.Errorf("backend pool %q not found for UDP listener %s", poolName, listenerCfg.Name)
	}

	// Create UDP proxy with simple round-robin balancer
	udpBalancer := l4.NewSimpleBalancer()
	udpConfig := l4.DefaultUDPProxyConfig()
	udpConfig.ListenAddr = listenerCfg.Address
	udpConfig.BackendPool = poolName

	udpProxy := l4.NewUDPProxy(pool, udpBalancer, udpConfig)

	if err := udpProxy.Start(); err != nil {
		return err
	}

	e.udpProxies = append(e.udpProxies, udpProxy)

	e.logger.Info("UDP listener started",
		logging.String("name", listenerCfg.Name),
		logging.String("address", listenerCfg.Address),
		logging.String("pool", poolName),
	)

	return nil
}

// startHTTPListener creates and starts an HTTP or HTTPS (L7) listener.
// If the listener has mTLS configured, it applies client certificate settings.
// Slow loris protection settings from the security package are applied.
func (e *Engine) startHTTPListener(listenerCfg *config.Listener) error {
	// Use security package slow loris defaults for hardened timeouts
	slp := security.DefaultSlowLorisProtection()

	opts := &listener.Options{
		Name:           listenerCfg.Name,
		Address:        listenerCfg.Address,
		Handler:        e.proxy,
		ReadTimeout:    slp.ReadTimeout,
		HeaderTimeout:  slp.ReadHeaderTimeout,
		WriteTimeout:   slp.WriteTimeout,
		IdleTimeout:    slp.IdleTimeout,
		MaxHeaderBytes: slp.MaxHeaderBytes,
	}

	var l listener.Listener
	var err error

	if listenerCfg.IsTLS() {
		// Check for mTLS configuration
		if listenerCfg.MTLS != nil && listenerCfg.MTLS.Enabled {
			l, err = e.createMTLSListener(opts, listenerCfg)
		} else {
			// Standard HTTPS listener
			l, err = listener.NewHTTPSListener(opts, e.tlsManager)
		}
	} else {
		// HTTP listener
		l, err = listener.NewHTTPListener(opts)
	}

	if err != nil {
		return fmt.Errorf("failed to create listener %s: %w", listenerCfg.Name, err)
	}

	if err := l.Start(); err != nil {
		return fmt.Errorf("failed to start listener %s: %w", listenerCfg.Name, err)
	}

	e.listeners = append(e.listeners, l)

	e.logger.Info("Listener started",
		logging.String("name", listenerCfg.Name),
		logging.String("address", l.Address()),
		logging.Bool("tls", listenerCfg.IsTLS()),
		logging.Bool("mtls", listenerCfg.MTLS != nil && listenerCfg.MTLS.Enabled),
	)

	return nil
}
