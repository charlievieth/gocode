package cache

import (
	"sync/atomic"
	"time"
)

// unixTime is a Unix timestamp
type unixTime int64

func newUnixTime(t time.Time) (u unixTime) {
	if !t.IsZero() {
		u = unixTime(t.UnixNano())
	}
	return u
}

func (u unixTime) Time() time.Time         { return time.Unix(0, int64(u)) }
func (u unixTime) Equal(t time.Time) bool  { return t.UnixNano() == int64(u) }
func (u unixTime) Before(t time.Time) bool { return int64(u) < t.UnixNano() }
func (u unixTime) After(t time.Time) bool  { return int64(u) > t.UnixNano() }
func (u unixTime) String() string          { return u.Time().String() }

// atomicUnixTime is an atomic version of unixTime
type atomicUnixTime struct {
	_ [0]func() // disallow non-atomic comparison
	u unixTime
}

func newAtomicUnixTime(t time.Time) atomicUnixTime {
	return atomicUnixTime{u: newUnixTime(t)}
}

func (u *atomicUnixTime) Set(t time.Time) {
	atomic.StoreInt64((*int64)(&u.u), int64(newUnixTime(t)))
}

func (u *atomicUnixTime) Load() unixTime {
	return unixTime(atomic.LoadInt64((*int64)(&u.u)))
}

func (u *atomicUnixTime) Time() time.Time         { return u.Load().Time() }
func (u *atomicUnixTime) Equal(t time.Time) bool  { return u.Load().Equal(t) }
func (u *atomicUnixTime) Before(t time.Time) bool { return u.Load().Before(t) }
func (u *atomicUnixTime) After(t time.Time) bool  { return u.Load().After(t) }
func (u *atomicUnixTime) String() string          { return u.Load().String() }
