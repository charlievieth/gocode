/*
Copyright 2013 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package lru implements an LRU cache.
package lru

import "sync"

// TODO(charle): delete this and replace with Cache
//
// LockingCache is an LRU cache. It is safe for concurrent access.
type LockingCache struct {
	mu    sync.Mutex
	cache map[string]*element
	ll    *list

	// MaxEntries is the maximum number of cache entries before
	// an item is evicted. Zero means no limit.
	MaxEntries int

	// OnEvicted optionally specifies a callback function to be
	// executed when an entry is purged from the cache.
	OnEvicted func(key string, value interface{})
}

type entry struct {
	key   string
	value interface{}
}

// NewLocking creates a new Cache.
// If maxEntries is zero, the cache has no limit and it's assumed
// that eviction is done by the caller.
func NewLocking(maxEntries int) *LockingCache {
	return &LockingCache{
		MaxEntries: maxEntries,
		ll:         newList(),
		cache:      make(map[string]*element),
	}
}

// Add adds a value to the cache.
func (c *LockingCache) Add(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[string]*element)
		c.ll = newList()
	}
	if ee, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ee)
		ee.value = value
		return
	}
	ele := c.ll.PushFront(entry{key, value})
	c.cache[key] = ele
	if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
		c.removeOldest()
	}
}

// WARN: remove if not used
func (c *LockingCache) Contains(key string) (ok bool) {
	c.mu.Lock()
	_, ok = c.cache[key]
	c.mu.Unlock()
	return ok
}

// Get looks up a key's value from the cache.
func (c *LockingCache) Get(key string) (value interface{}, ok bool) {
	c.mu.Lock()
	var ele *element
	if ele, ok = c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		value = ele.value
	}
	c.mu.Unlock()
	return value, ok
}

// Get looks up a key's value from the cache but takes the key a byte slice
// so that we don't have to allocate a string.
func (c *LockingCache) GetB(key []byte) (value interface{}, ok bool) {
	c.mu.Lock()
	var ele *element
	if ele, ok = c.cache[string(key)]; ok {
		c.ll.MoveToFront(ele)
		value = ele.value
	}
	c.mu.Unlock()
	return value, ok
}

// Remove removes the provided key from the cache.
func (c *LockingCache) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache != nil {
		if ele, hit := c.cache[key]; hit {
			c.removeElement(ele)
		}
	}
}

// removeOldest removes the oldest item from the cache.
func (c *LockingCache) removeOldest() {
	if c.cache == nil {
		return
	}
	ele := c.ll.Back()
	if ele != nil {
		c.removeElement(ele)
	}
}

// RemoveOldest removes the oldest item from the cache.
func (c *LockingCache) RemoveOldest() {
	c.mu.Lock()
	c.removeOldest()
	c.mu.Unlock()
}

func (c *LockingCache) removeElement(e *element) {
	c.ll.Remove(e)
	kv := e.entry
	delete(c.cache, kv.key)
	if c.OnEvicted != nil {
		c.OnEvicted(kv.key, kv.value)
	}
}

// Len returns the number of items in the cache.
func (c *LockingCache) Len() int {
	c.mu.Lock()
	// TODO: this previosly used: `n = c.ll.Len()` - why?
	n := len(c.cache)
	c.mu.Unlock()
	return n
}

// Clear purges all stored items from the cache.
func (c *LockingCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.OnEvicted != nil {
		for _, e := range c.cache {
			kv := e.entry
			c.OnEvicted(kv.key, kv.value)
		}
	}
	c.ll = nil
	c.cache = nil
}

func (c *LockingCache) ForEach(fn func(key string, value interface{}) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, ele := range c.cache {
		if !fn(key, ele.value) {
			break
		}
	}
}

func (c *LockingCache) RemoveFunc(fn func(key string, value interface{}) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, ele := range c.cache {
		if fn(key, ele.value) {
			c.removeElement(ele)
		}
	}
}

// TODO: remove if not used
//
// Cache is an LRU cache. It is not safe for concurrent access.
type Cache struct {
	cache map[string]*element
	ll    *list

	// MaxEntries is the maximum number of cache entries before
	// an item is evicted. Zero means no limit.
	MaxEntries int

	// OnEvicted optionally specifies a callback function to be
	// executed when an entry is purged from the cache.
	OnEvicted func(key string, value interface{})
}

// NewLocking creates a new Cache.
// If maxEntries is zero, the cache has no limit and it's assumed
// that eviction is done by the caller.
func New(maxEntries int) *Cache {
	return &Cache{
		MaxEntries: maxEntries,
		ll:         newList(),
		cache:      make(map[string]*element),
	}
}

// Add adds a value to the cache.
func (c *Cache) Add(key string, value interface{}) {
	if c.cache == nil {
		c.cache = make(map[string]*element)
		c.ll = newList()
	}
	if ee, ok := c.cache[key]; ok {
		c.ll.MoveToFront(ee)
		ee.value = value
		return
	}
	ele := c.ll.PushFront(entry{key, value})
	c.cache[key] = ele
	if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
		c.removeOldest()
	}
}

// WARN: remove if not used
func (c *Cache) Contains(key string) (ok bool) {
	_, ok = c.cache[key]
	return ok
}

// Get looks up a key's value from the cache.
func (c *Cache) Get(key string) (value interface{}, ok bool) {
	var ele *element
	if ele, ok = c.cache[key]; ok {
		c.ll.MoveToFront(ele)
		value = ele.value
	}
	return value, ok
}

// Get looks up a key's value from the cache but takes the key a byte slice
// so that we don't have to allocate a string.
func (c *Cache) GetB(key []byte) (value interface{}, ok bool) {
	var ele *element
	if ele, ok = c.cache[string(key)]; ok {
		c.ll.MoveToFront(ele)
		value = ele.value
	}
	return value, ok
}

// Remove removes the provided key from the cache.
func (c *Cache) Remove(key string) {
	if c.cache != nil {
		if ele, hit := c.cache[key]; hit {
			c.removeElement(ele)
		}
	}
}

// removeOldest removes the oldest item from the cache.
func (c *Cache) removeOldest() {
	if c.cache == nil {
		return
	}
	ele := c.ll.Back()
	if ele != nil {
		c.removeElement(ele)
	}
}

// RemoveOldest removes the oldest item from the cache.
func (c *Cache) RemoveOldest() {
	c.removeOldest()
}

func (c *Cache) removeElement(e *element) {
	c.ll.Remove(e)
	kv := e.entry
	delete(c.cache, kv.key)
	if c.OnEvicted != nil {
		c.OnEvicted(kv.key, kv.value)
	}
}

// Len returns the number of items in the cache.
func (c *Cache) Len() int {
	// TODO: this previosly used: `n = c.ll.Len()` - why?
	return len(c.cache)
}

// Clear purges all stored items from the cache.
func (c *Cache) Clear() {
	if c.OnEvicted != nil {
		for _, e := range c.cache {
			kv := e.entry
			c.OnEvicted(kv.key, kv.value)
		}
	}
	c.ll = nil
	c.cache = nil
}

func (c *Cache) RemoveFunc(fn func(key string, value interface{}) bool) {
	for key, ele := range c.cache {
		if fn(key, ele.value) {
			c.removeElement(ele)
		}
	}
}
