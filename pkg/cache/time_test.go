package cache

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAtomicUnixTime(t *testing.T) {
	now := time.Now().UTC()
	u := new(atomicUnixTime)
	u.Set(now)
	if !u.Equal(now) {
		t.Errorf("Equal(%s) = %t, want: %t", u, true, false)
	}
	if !u.Time().Equal(now) {
		t.Errorf("Time.Equal(%s) = %t, want: %t", u, true, false)
	}
	if got, want := u.String(), now.In(time.Local).String(); got != want {
		t.Errorf("String(%s) = %q, want: %q", u, got, want)
	}
	// Make sure since is relatively accurate
	if d := u.Since(); d > time.Millisecond*10 {
		t.Errorf("Since(%s) = %s, want: %s", u, d, time.Second)
	}
	u.Set(now.Add(time.Second))
	if u.Equal(now) {
		t.Errorf("Equal(%s) = %t, want: %t", u, false, true)
	}
	if u.Before(now) {
		t.Errorf("Before(%s) = %t, want: %t", u, true, false)
	}
	if !u.After(now) {
		t.Errorf("After(%s) = %t, want: %t", u, false, true)
	}
}

func TestAtomicUnixTimeParallel(t *testing.T) {
	u := newAtomicUnixTime(time.Now())
	now := int64(u.Time().UnixNano())

	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10_000; i++ {
				u.Set(time.Unix(0, atomic.AddInt64(&now, 1)))
				_ = u.Load()
			}
		}()
	}
	wg.Wait()

	want := time.Unix(0, atomic.LoadInt64(&now))
	if !u.Equal(want) {
		t.Errorf("got: %s; want: %s", &u, want)
	}
}
