package connlog

// ProxyHook adapts *Store to proxy.ConnRecorder without importing proxy
// (proxy.ConnEvent fields are passed via RecordProxy).
// Prefer wiring with internal/proxy bridge in main — see RecordFunc.

// RecordFunc is a function adapter for connection recording.
type RecordFunc func(proto, clientIP, target, host string, port int, via, rule string, ok bool, errMsg string, durationMs int64)

// Hook implements a callable that stores fields into Store.
func (s *Store) Hook() RecordFunc {
	return func(proto, clientIP, target, host string, port int, via, rule string, ok bool, errMsg string, durationMs int64) {
		s.FromFields(proto, clientIP, target, host, port, via, rule, ok, errMsg, durationMs)
	}
}
