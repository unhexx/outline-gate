package connlog

import (
	"sync"
	"testing"
	"time"
)

func TestRingOverwrite(t *testing.T) {
	s := New(3)
	for i := 0; i < 5; i++ {
		s.Record(Event{Host: string(rune('a' + i)), Via: ViaTunnel, OK: true})
	}
	if s.Len() != 3 {
		t.Fatalf("len=%d", s.Len())
	}
	snap := s.Snapshot(0)
	if len(snap) != 3 {
		t.Fatalf("snap len=%d", len(snap))
	}
	// oldest kept should be events 3,4,5 → hosts c,d,e (indices 2,3,4)
	if snap[0].Host != "c" || snap[2].Host != "e" {
		t.Fatalf("hosts=%v %v %v", snap[0].Host, snap[1].Host, snap[2].Host)
	}
	if snap[0].ID >= snap[1].ID || snap[1].ID >= snap[2].ID {
		t.Fatalf("ids not increasing: %d %d %d", snap[0].ID, snap[1].ID, snap[2].ID)
	}
}

func TestSnapshotLimit(t *testing.T) {
	s := New(10)
	for i := 0; i < 5; i++ {
		s.Record(Event{Port: i, OK: true})
	}
	snap := s.Snapshot(2)
	if len(snap) != 2 || snap[0].Port != 3 || snap[1].Port != 4 {
		t.Fatalf("%+v", snap)
	}
}

func TestSubscribeReceives(t *testing.T) {
	s := New(10)
	ch, unsub := s.Subscribe()
	defer unsub()

	s.Record(Event{Host: "x", Via: ViaDirect, OK: true})
	select {
	case e := <-ch:
		if e.Host != "x" || e.Via != ViaDirect || e.ID == 0 {
			t.Fatalf("%+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestConcurrentRecord(t *testing.T) {
	s := New(100)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Record(Event{OK: true, Via: ViaTunnel})
		}()
	}
	wg.Wait()
	if s.Len() != 50 {
		t.Fatalf("len=%d", s.Len())
	}
	// IDs unique and sequential max
	snap := s.Snapshot(0)
	seen := make(map[uint64]struct{})
	for _, e := range snap {
		if _, ok := seen[e.ID]; ok {
			t.Fatalf("duplicate id %d", e.ID)
		}
		seen[e.ID] = struct{}{}
	}
}

func TestStats(t *testing.T) {
	s := New(10)
	s.Record(Event{Via: ViaTunnel, OK: true, Time: time.Now().UTC()})
	s.Record(Event{Via: ViaDirect, OK: false, Time: time.Now().UTC()})
	total, vpn, direct, ok, fail := s.Stats(time.Now().Add(-time.Minute))
	if total != 2 || vpn != 1 || direct != 1 || ok != 1 || fail != 1 {
		t.Fatalf("total=%d vpn=%d direct=%d ok=%d fail=%d", total, vpn, direct, ok, fail)
	}
}
