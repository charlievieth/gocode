package cache

import "testing"

func BenchmarkMatchCache(b *testing.B) {
	var m matchCache
	for i := 0; i < b.N; i++ {
		for id := ContextKeyID(0); id < 5; id++ {
			m.Set(id, true)
		}
		m = matchCache{}
	}
}
