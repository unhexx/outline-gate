package proxy

import (
	"net"
	"sync"

	"github.com/unhexx/outline-gate/internal/routing"
)

// EnginePathDecider implements PathDecider using a live routing.Engine.
// It maps routing.Path (int) to proxy path strings via routing.Path.String().
type EnginePathDecider struct {
	Mu     *sync.Mutex
	Engine func() *routing.Engine
	// Bypass optionally labels Direct matches (for connection log).
	Bypass BypassMatcher
}

// DecidePath implements PathDecider.
func (d *EnginePathDecider) DecidePath(dst net.IP) (via string, rule string) {
	if d == nil || dst == nil {
		return PathTunnel, ""
	}
	var eng *routing.Engine
	if d.Mu != nil {
		d.Mu.Lock()
		if d.Engine != nil {
			eng = d.Engine()
		}
		d.Mu.Unlock()
	} else if d.Engine != nil {
		eng = d.Engine()
	}
	if eng == nil {
		return PathTunnel, ""
	}
	switch eng.Decide(dst) {
	case routing.PathDrop:
		return PathDrop, "policy:drop"
	case routing.PathDirect:
		if eng.IsServerIP(dst) {
			return PathDirect, "outline-server"
		}
		if d.Bypass != nil {
			if ok, r := d.Bypass.MatchBypass(dst.String()); ok {
				return PathDirect, r
			}
		}
		return PathDirect, "policy:direct"
	default:
		return PathTunnel, ""
	}
}
