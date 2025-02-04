package xlru

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

var getTests = []struct {
	name       string
	keyToAdd   string
	keyToGet   string
	expectedOk bool
}{
	{"string_hit", "myKey", "myKey", true},
	{"string_miss", "myKey", "nonsense", false},
	{"simple_struct_hit", `simpleStruct{1, "two"}`, `simpleStruct{1, "two"}`, true},
	{"simple_struct_miss", `simpleStruct{1, "two"}`, `simpleStruct{0, "noway"}`, false},
	{"complex_struct_hit", `complexStruct{1, simpleStruct{2, "three"}}`, `complexStruct{1, simpleStruct{2, "three"}}`, true},
}

func TestGet(t *testing.T) {
	for _, tt := range getTests {
		lru := New[string, int](0)
		lru.Add(tt.keyToAdd, 1234)
		val, ok := lru.Get(tt.keyToGet)
		if ok != tt.expectedOk {
			t.Fatalf("%s: cache hit = %v; want %v", tt.name, ok, !ok)
		} else if ok && val != 1234 {
			t.Fatalf("%s expected get to return 1234 but got %v", tt.name, val)
		}
	}
}

func TestRemove(t *testing.T) {
	lru := New[string, int](0)
	lru.Add("myKey", 1234)
	if val, ok := lru.Get("myKey"); !ok {
		t.Fatal("TestRemove returned no match")
	} else if val != 1234 {
		t.Fatalf("TestRemove failed.  Expected %d, got %v", 1234, val)
	}

	lru.Remove("myKey")
	if _, ok := lru.Get("myKey"); ok {
		t.Fatal("TestRemove returned a removed entry")
	}
}

func TestRemoveOldest(t *testing.T) {
	lru := New[string, int](0)
	want := make([]bool, 4)
	keys := make([]string, 4)
	for i := 0; i < len(want); i++ {
		want[i] = true
		keys[i] = fmt.Sprintf("%d", i)
		lru.Add(keys[i], i)
		if _, ok := lru.Get(keys[i]); !ok {
			t.Fatal("TestRemoveOldest returned no match")
		}
	}
	for i := 0; i < len(want); i++ {
		want[i] = false
		lru.RemoveOldest()
		for j, exp := range want {
			_, got := lru.Get(keys[j])
			if got != exp {
				t.Errorf("RemoveOldest(%q) = %t; want: %t", keys[i], got, exp)
			}
		}
	}
	if lru.Len() != 0 {
		t.Errorf("TestRemoveOldest len should be 0 got: %d", lru.Len())
	}
}

func TestClear(t *testing.T) {
	lru := New[string, int](0)
	for i := 0; i < 4; i++ {
		lru.Add(fmt.Sprintf("%d", i), i)
	}
	if lru.Len() != 4 {
		t.Errorf("Len = %d; want: %d", lru.Len(), 4)
	}
	lru.Clear()
	if lru.Len() != 0 {
		t.Errorf("Len = %d; want: %d", lru.Len(), 0)
	}
}

func TestTrim(t *testing.T) {
	lru := New[string, int](8)
	for i := 0; i < 8; i++ {
		lru.Add(fmt.Sprintf("key_%d", i), i)
	}
	if lru.Len() != 8 {
		t.Errorf("Len = %d; want %d", lru.Len(), 8)
	}
	lru.Trim(4)
	if lru.Len() != 4 {
		t.Errorf("Len = %d; want %d", lru.Len(), 4)
	}
	for i := 4; i < 8; i++ {
		key := fmt.Sprintf("key_%d", i)
		_, ok := lru.Get(key)
		if !ok {
			t.Errorf("Get(%q) = %t; want: %t", key, ok, true)
		}
	}
}

func TestRemoveFunc(t *testing.T) {
	lru := New[string, int](0)
	lru.Add("a", 1)
	lru.Add("b", 2)
	lru.RemoveFunc(func(key string, val int) bool {
		return key == "b"
	})
	if _, ok := lru.Get("b"); ok {
		t.Error("Failed to remove key: b")
	}
	if _, ok := lru.Get("a"); !ok {
		t.Error("Should not have removed key: a")
	}
}

func TestParallelStress(t *testing.T) {
	const N = 1024
	lru := New[string, int](1024)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 128*1024; i++ {
				key := strconv.Itoa(i % N)
				switch i % 4 {
				case 0:
					lru.Add(key, i)
				case 1:
					lru.Remove(key)
				case 2:
					lru.RemoveOldest()
				case 3:
					lru.Clear()
					lru.Len()
				}
			}
		}()
	}
	wg.Wait()
}

var benchKeys [256]string

func init() {
	keys := &benchKeys
	for i := 0; i < len(keys); i++ {
		keys[i] = "key_" + strconv.Itoa(i)
	}
}

func BenchmarkGet(b *testing.B) {
	keys := &benchKeys
	c := New[string, int](len(keys))
	for i := 0; i < len(keys); i++ {
		c.Add(keys[i], i)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.Get(keys[i%len(keys)])
	}
}

// func BenchmarkGetParallel(b *testing.B) {
// 	keys := &benchKeys
// 	c := New(len(keys))
// 	for i := 0; i < len(keys); i++ {
// 		c.Add(keys[i], i)
// 	}
// 	b.ResetTimer()

// 	b.RunParallel(func(pb *testing.PB) {
// 		for i := 0; pb.Next(); i++ {
// 			c.Get(keys[i%len(keys)])
// 		}
// 	})
// }

func BenchmarkAdd(b *testing.B) {
	keys := &benchKeys
	b.Run("N/1", func(b *testing.B) {
		c := New[string, int](len(keys))
		for i := 0; i < b.N; i++ {
			key := keys[i%len(keys)]
			c.Add(key, i)
		}
	})

	b.Run("N/2", func(b *testing.B) {
		c := New[string, int](len(keys) / 2)
		for i := 0; i < b.N; i++ {
			key := keys[i%len(keys)]
			c.Add(key, i)
		}
	})

	b.Run("N/4", func(b *testing.B) {
		c := New[string, int](len(keys) / 4)
		for i := 0; i < b.N; i++ {
			key := keys[i%len(keys)]
			c.Add(key, i)
		}
	})
}

// func BenchmarkAddParallel(b *testing.B) {
// 	keys := &benchKeys
// 	b.Run("N/1", func(b *testing.B) {
// 		c := New(len(keys))
// 		b.RunParallel(func(pb *testing.PB) {
// 			for i := 0; pb.Next(); i++ {
// 				c.Add(keys[i%len(keys)], i)
// 			}
// 		})
// 	})

// 	b.Run("N/2", func(b *testing.B) {
// 		c := New(len(keys) / 2)
// 		b.RunParallel(func(pb *testing.PB) {
// 			for i := 0; pb.Next(); i++ {
// 				c.Add(keys[i%len(keys)], i)
// 			}
// 		})
// 	})

// 	b.Run("N/4", func(b *testing.B) {
// 		c := New(len(keys) / 4)
// 		b.RunParallel(func(pb *testing.PB) {
// 			for i := 0; pb.Next(); i++ {
// 				c.Add(keys[i%len(keys)], i)
// 			}
// 		})
// 	})
// }
