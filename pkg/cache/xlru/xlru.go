package xlru

import "sync"

type element[K comparable, V any] struct {
	// Next and previous pointers in the doubly-linked list of elements.
	// To simplify the implementation, internally a list l is implemented
	// as a ring, such that &l.root is both the next element of the last
	// list element (l.Back()) and the previous element of the first list
	// element (l.Front()).
	next, prev *element[K, V]

	// The value stored with this element.
	key   K
	value V
}

type Cache[K comparable, V any] struct {
	mu    sync.Mutex
	cache map[K]*element[K, V]
	ll    *list[K, V]

	// MaxEntries is the maximum number of cache entries before
	// an item is evicted. Zero means no limit.
	MaxEntries int

	// OnEvicted optionally specifies a callback function to be
	// executed when an entry is purged from the cache.
	// OnEvicted func(key string, value interface{})
}

func New[K comparable, V any](maxEntries int) *Cache[K, V] {
	return &Cache[K, V]{
		MaxEntries: maxEntries,
		ll:         newList[K, V](),
		cache:      make(map[K]*element[K, V]),
	}
}

// Add adds a value to the cache.
func (c *Cache[K, V]) Add(key K, value V) {
	c.mu.Lock()
	if c.cache == nil {
		c.cache = make(map[K]*element[K, V])
		c.ll = newList[K, V]()
	}
	if ee, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ee)
		ee.value = value
	} else {
		ele := c.ll.PushFront(key, value)
		c.cache[key] = ele
		if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
			c.removeOldest()
		}
	}
	c.mu.Unlock()
}

// Get looks up a key's value from the cache.
func (c *Cache[K, V]) Get(key K) (value V, ok bool) {
	c.mu.Lock()
	var ele *element[K, V]
	if ele, ok = c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		value = ele.value
	}
	c.mu.Unlock()
	return value, ok
}

// Remove removes the provided key from the cache.
func (c *Cache[K, V]) Remove(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache != nil {
		if ele, hit := c.cache[key]; hit {
			c.removeElement(ele)
		}
	}
}

// removeOldest removes the oldest item from the cache.
func (c *Cache[K, V]) removeOldest() {
	if c.cache == nil {
		return
	}
	ele := c.ll.Back()
	if ele != nil {
		c.removeElement(ele)
	}
}

// RemoveOldest removes the oldest item from the cache.
func (c *Cache[K, V]) RemoveOldest() {
	c.mu.Lock()
	c.removeOldest()
	c.mu.Unlock()
}

// Trim shrinks the cache to size elements.
func (c *Cache[K, V]) Trim(size int) {
	if c == nil || size < 0 {
		return
	}
	c.mu.Lock()
	for len(c.cache) > size {
		c.removeOldest()
	}
	c.mu.Unlock()
}

func (c *Cache[K, V]) removeElement(e *element[K, V]) {
	c.ll.Remove(e)
	key := e.key
	// value := e.value
	delete(c.cache, key)
	// if c.OnEvicted != nil {
	// 	c.OnEvicted(kv.key, kv.value)
	// }
}

// Len returns the number of items in the cache.
func (c *Cache[K, V]) Len() (n int) {
	if c != nil {
		c.mu.Lock()
		// TODO: this previosly used: `n = c.ll.Len()` - why?
		n = len(c.cache)
		c.mu.Unlock()
	}
	return n
}

// Clear purges all stored items from the cache.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	// if c.OnEvicted != nil {
	// 	for _, e := range c.cache {
	// 		kv := e.entry
	// 		c.OnEvicted(kv.key, kv.value)
	// 	}
	// }
	c.ll = nil
	c.cache = nil
}

func (c *Cache[K, V]) ForEach(fn func(key K, value V) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, ele := range c.cache {
		if !fn(key, ele.value) {
			break
		}
	}
}

func (c *Cache[K, V]) RemoveFunc(fn func(key K, value V) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, ele := range c.cache {
		if fn(key, ele.value) {
			c.removeElement(ele)
		}
	}
}
