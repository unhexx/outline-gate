package proxy

// StoreHook adapts a function to ConnRecorder (used to wire connlog.Store).
type StoreHook struct {
	Fn func(e ConnEvent)
}

// RecordConnect implements ConnRecorder.
func (h *StoreHook) RecordConnect(e ConnEvent) {
	if h == nil || h.Fn == nil {
		return
	}
	h.Fn(e)
}

// NewStoreHook wraps fn as a ConnRecorder.
func NewStoreHook(fn func(e ConnEvent)) ConnRecorder {
	if fn == nil {
		return nil
	}
	return &StoreHook{Fn: fn}
}
