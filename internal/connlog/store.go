// Package connlog provides an in-memory ring buffer of proxy connection events
// for the management UI (live routing log).
package connlog

import (
	"strings"
	"sync"
	"time"
)

// Proto is the entry surface that handled the connection.
type Proto string

const (
	ProtoSOCKS Proto = "socks"
	ProtoL3    Proto = "l3"
)

// Via is the egress path chosen for the connection.
type Via string

const (
	ViaTunnel Via = "tunnel"
	ViaDirect Via = "direct"
	ViaDrop   Via = "drop"
)

// DefaultTTL is how long events stay in the ring when capacity is not full.
const DefaultTTL = time.Hour

// Event is a single connection attempt recorded for the UI.
type Event struct {
	ID         uint64    `json:"id"`
	Time       time.Time `json:"time"`
	Proto      Proto     `json:"proto"`
	ClientIP   string    `json:"client_ip"`
	Target     string    `json:"target"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Via        Via       `json:"via"`
	Rule       string    `json:"rule,omitempty"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms,omitempty"`
}

// Store is a fixed-capacity ring buffer with non-blocking fan-out to subscribers.
// Events older than TTL are pruned on write/read (in addition to capacity).
type Store struct {
	mu   sync.RWMutex
	cap  int
	ttl  time.Duration
	buf  []Event
	seq  uint64
	subs map[chan Event]struct{}
}

// New creates a Store with the given capacity (minimum 1; default 500 if <= 0)
// and DefaultTTL (1h).
func New(capacity int) *Store {
	return NewWithTTL(capacity, DefaultTTL)
}

// NewWithTTL creates a Store with capacity and event TTL.
// ttl <= 0 disables time-based pruning (capacity-only ring).
func NewWithTTL(capacity int, ttl time.Duration) *Store {
	if capacity <= 0 {
		capacity = 500
	}
	return &Store{
		cap:  capacity,
		ttl:  ttl,
		buf:  make([]Event, 0, capacity),
		subs: make(map[chan Event]struct{}),
	}
}

// Capacity returns the ring size.
func (s *Store) Capacity() int {
	if s == nil {
		return 0
	}
	return s.cap
}

// Len returns how many events are currently stored.
func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	return len(s.buf)
}

// Record assigns ID/Time (if zero), appends to the ring, and notifies subscribers.
// Returns the stored event.
func (s *Store) Record(e Event) Event {
	if s == nil {
		return e
	}
	s.mu.Lock()
	s.seq++
	e.ID = s.seq
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	s.pruneLocked(e.Time)
	if len(s.buf) < s.cap {
		s.buf = append(s.buf, e)
	} else {
		// drop oldest
		copy(s.buf[0:], s.buf[1:])
		s.buf[len(s.buf)-1] = e
	}
	// snapshot subscribers under lock
	subs := make([]chan Event, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
			// slow subscriber: drop this event for them
		}
	}
	return e
}

// pruneLocked drops events older than TTL. Caller must hold s.mu.
func (s *Store) pruneLocked(now time.Time) {
	if s.ttl <= 0 || len(s.buf) == 0 {
		return
	}
	cutoff := now.Add(-s.ttl)
	// find first event still within TTL (buf is chronological)
	i := 0
	for i < len(s.buf) && s.buf[i].Time.Before(cutoff) {
		i++
	}
	if i == 0 {
		return
	}
	if i >= len(s.buf) {
		s.buf = s.buf[:0]
		return
	}
	copy(s.buf[0:], s.buf[i:])
	s.buf = s.buf[:len(s.buf)-i]
}

// Snapshot returns up to limit most recent events in chronological order
// (oldest → newest). limit <= 0 means all stored events.
func (s *Store) Snapshot(limit int) []Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	n := len(s.buf)
	if n == 0 {
		return nil
	}
	if limit <= 0 || limit > n {
		limit = n
	}
	start := n - limit
	out := make([]Event, limit)
	copy(out, s.buf[start:])
	return out
}

// Stats returns counts for events with Time >= since.
func (s *Store) Stats(since time.Time) (total, vpn, direct, ok, fail int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	for i := range s.buf {
		e := &s.buf[i]
		if !since.IsZero() && e.Time.Before(since) {
			continue
		}
		total++
		if e.Via == ViaDirect {
			direct++
		} else if e.Via == ViaDrop {
			// count as neither vpn nor direct for UI totals; still in total
		} else {
			vpn++
		}
		if e.OK {
			ok++
		} else {
			fail++
		}
	}
	return
}

// Subscribe returns a channel of live events and an unsubscribe function.
// The channel is buffered (32); slow consumers may miss events.
func (s *Store) Subscribe() (<-chan Event, func()) {
	if s == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan Event, 32)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	unsub := func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
	return ch, unsub
}

// FromFields records an event from loose proxy fields (avoids import cycle with proxy).
func (s *Store) FromFields(proto, clientIP, target, host string, port int, via, rule string, ok bool, errMsg string, durationMs int64) Event {
	if s == nil {
		return Event{}
	}
	p := Proto(strings.ToLower(proto))
	if p != ProtoSOCKS && p != ProtoL3 {
		p = Proto(proto)
	}
	v := Via(strings.ToLower(via))
	switch v {
	case ViaTunnel, ViaDirect, ViaDrop:
	default:
		v = ViaTunnel
	}
	return s.Record(Event{
		Proto:      p,
		ClientIP:   clientIP,
		Target:     target,
		Host:       host,
		Port:       port,
		Via:        v,
		Rule:       rule,
		OK:         ok,
		Error:      errMsg,
		DurationMs: durationMs,
	})
}
