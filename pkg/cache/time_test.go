package cache

import (
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
