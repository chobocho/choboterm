package main

import (
	"sync"
	"testing"
	"time"
)

// Output after a quiet spell (a key echo) is emitted at once, not on the next tick,
// while a burst of small reads is still batched.
func TestPumpEchoLatencyAndBatching(t *testing.T) {
	var (
		mu    sync.Mutex
		emits int
	)
	got := make(chan time.Time, 1024)
	a := NewApp()
	a.hooks.emit = func(name string, data ...interface{}) {
		if name == "term:data" {
			mu.Lock()
			emits++
			mu.Unlock()
			got <- time.Now()
		}
	}
	sess := newPipeSession()
	tb := a.getTab(1)
	tb.sess = sess
	go tb.pump(sess)
	defer sess.out.Close()

	// Key echoes, spaced wider than the flush interval.
	const echoes = 20
	var total time.Duration
	for i := 0; i < echoes; i++ {
		// Spread the writes over the tick phase, as real keystrokes are; spin so
		// the coarse Windows sleep doesn't line them up with the ticker.
		time.Sleep(20 * time.Millisecond)
		until := time.Now().Add(time.Duration(i%16) * time.Millisecond)
		for time.Now().Before(until) {
		}
		start := time.Now()
		sess.out.Write([]byte("a"))
		select {
		case at := <-got:
			total += at.Sub(start)
		case <-time.After(time.Second):
			t.Fatal("echo not emitted")
		}
	}
	// Waiting for a 16ms ticker averages ~8ms; emitting at once is well under that.
	t.Logf("average echo latency %v", total/echoes)
	if avg := total / echoes; avg > 4*time.Millisecond {
		t.Fatalf("average echo latency %v", avg)
	}

	// A burst of many small reads is coalesced into far fewer events.
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	emits = 0
	mu.Unlock()
	const writes = 2000
	for i := 0; i < writes; i++ {
		sess.out.Write([]byte("0123456789"))
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := emits
	mu.Unlock()
	if n == 0 || n > writes/10 {
		t.Fatalf("%d writes produced %d events", writes, n)
	}
}
