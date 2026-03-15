package engine

import (
	"fmt"
	"strings"

	"github.com/openloadbalancer/olb/internal/admin"
	"github.com/openloadbalancer/olb/internal/backend"
	"github.com/openloadbalancer/olb/internal/config"
	"github.com/openloadbalancer/olb/internal/mcp"
	"github.com/openloadbalancer/olb/internal/router"
	olbTLS "github.com/openloadbalancer/olb/internal/tls"
)

// --------------------------------------------------------------------------
// Admin API adapters — these bridge engine components to admin.Server interfaces
// --------------------------------------------------------------------------

// engineConfigGetter implements admin.ConfigGetter by returning the engine's
// current configuration.
type engineConfigGetter struct {
	engine *Engine
}

func (g *engineConfigGetter) GetConfig() interface{} {
	return g.engine.GetConfig()
}

// engineCertLister implements admin.CertLister using the TLS manager.
type engineCertLister struct {
	tlsMgr *olbTLS.Manager
}

func (l *engineCertLister) ListCertificates() []admin.CertInfoView {
	certs := l.tlsMgr.ListCertificates()
	views := make([]admin.CertInfoView, len(certs))
	for i, c := range certs {
		views[i] = admin.CertInfoView{
			Names:      c.Names,
			Expiry:     c.Expiry,
			IsWildcard: c.IsWildcard,
		}
	}
	return views
}

// --------------------------------------------------------------------------
// MCP provider adapters — these bridge engine components to mcp.Server interfaces
// --------------------------------------------------------------------------

// engineMetricsProvider implements mcp.MetricsProvider.
type engineMetricsProvider struct {
	registry interface {
		// We use an interface to avoid coupling to the concrete metrics.Registry
		// methods beyond what MCP needs.
	}
}

func (p *engineMetricsProvider) QueryMetrics(pattern string) map[string]interface{} {
	// Return basic info; full metrics integration can be extended later.
	return map[string]interface{}{
		"pattern": pattern,
		"message": "metrics query via MCP",
	}
}

// engineBackendProvider implements mcp.BackendProvider.
type engineBackendProvider struct {
	poolMgr *backend.PoolManager
}

func (p *engineBackendProvider) ListPools() []mcp.PoolInfo {
	pools := p.poolMgr.GetAllPools()
	result := make([]mcp.PoolInfo, 0, len(pools))
	for _, pool := range pools {
		backends := pool.GetAllBackends()
		backendInfos := make([]mcp.BackendInfo, 0, len(backends))
		for _, b := range backends {
			backendInfos = append(backendInfos, mcp.BackendInfo{
				ID:          b.ID,
				Address:     b.Address,
				Status:      b.State().String(),
				Weight:      int(b.Weight),
				Connections: b.ActiveConns(),
			})
		}
		result = append(result, mcp.PoolInfo{
			Name:      pool.Name,
			Algorithm: pool.Algorithm,
			Backends:  backendInfos,
		})
	}
	return result
}

func (p *engineBackendProvider) ModifyBackend(action, poolName, addr string) error {
	pool := p.poolMgr.GetPool(poolName)
	if pool == nil {
		return fmt.Errorf("pool %q not found", poolName)
	}

	switch strings.ToLower(action) {
	case "add":
		b := backend.NewBackend(addr, addr)
		return pool.AddBackend(b)
	case "remove":
		return pool.RemoveBackend(addr)
	case "drain":
		return pool.DrainBackend(addr)
	case "enable":
		b := pool.GetBackend(addr)
		if b == nil {
			return fmt.Errorf("backend %q not found in pool %q", addr, poolName)
		}
		b.SetState(backend.StateUp)
		return nil
	case "disable":
		b := pool.GetBackend(addr)
		if b == nil {
			return fmt.Errorf("backend %q not found in pool %q", addr, poolName)
		}
		b.SetState(backend.StateDown)
		return nil
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

// engineConfigProvider implements mcp.ConfigProvider.
type engineConfigProvider struct {
	engine *Engine
}

func (p *engineConfigProvider) GetConfig() interface{} {
	return p.engine.GetConfig()
}

// engineRouteProvider implements mcp.RouteProvider.
type engineRouteProvider struct {
	rtr *router.Router
}

func (p *engineRouteProvider) ModifyRoute(action, host, path, backendPool string) error {
	switch strings.ToLower(action) {
	case "add":
		route := &router.Route{
			Name:        fmt.Sprintf("%s-%s", host, path),
			Host:        host,
			Path:        path,
			BackendPool: backendPool,
		}
		return p.rtr.AddRoute(route)
	case "remove":
		routeName := fmt.Sprintf("%s-%s", host, path)
		p.rtr.RemoveRoute(routeName)
		return nil
	case "update":
		// Remove old, add new
		routeName := fmt.Sprintf("%s-%s", host, path)
		p.rtr.RemoveRoute(routeName) // ignore if not found
		route := &router.Route{
			Name:        routeName,
			Host:        host,
			Path:        path,
			BackendPool: backendPool,
		}
		return p.rtr.AddRoute(route)
	default:
		return fmt.Errorf("unknown route action: %s", action)
	}
}

// --------------------------------------------------------------------------
// Helper: getMCPAddress returns the MCP transport address.
// Uses a port offset from the admin address (admin port + 1).
// --------------------------------------------------------------------------

func getMCPAddress(cfg *config.Config) string {
	adminAddr := getAdminAddress(cfg)
	// Parse port from admin address and use port+1 for MCP
	// Default: if admin is :8080, MCP will be :8081
	if adminAddr == "" {
		return ""
	}

	// Find the last colon to get the port
	idx := strings.LastIndex(adminAddr, ":")
	if idx < 0 {
		return ""
	}

	host := adminAddr[:idx]
	portStr := adminAddr[idx+1:]
	var port int
	for _, ch := range portStr {
		if ch >= '0' && ch <= '9' {
			port = port*10 + int(ch-'0')
		} else {
			return "" // non-numeric port
		}
	}

	return fmt.Sprintf("%s:%d", host, port+1)
}
