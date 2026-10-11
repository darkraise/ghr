// Package lru is a map that forgets its least recently used entries.
package lru

import "container/list"

// Cache is a map bounded by entry count and, when maxSize is above 0, by the
// total of the sizes given to Add; the least recently used entries go
// first. Callers hold their own lock.
type Cache[K comparable, V any] struct {
	maxEntries int
	maxSize    int64
	size       int64
	order      *list.List // front is the most recently used
	items      map[K]*list.Element
}

type item[K comparable, V any] struct {
	key  K
	val  V
	size int64
}

func New[K comparable, V any](maxEntries int, maxSize int64) *Cache[K, V] {
	return &Cache[K, V]{maxEntries: maxEntries, maxSize: maxSize, order: list.New(), items: map[K]*list.Element{}}
}

func (c *Cache[K, V]) Get(k K) (V, bool) {
	e, ok := c.items[k]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*item[K, V]).val, true
}

// Add stores v under k. An entry larger than maxSize on its own is not kept.
func (c *Cache[K, V]) Add(k K, v V, size int64) {
	c.Remove(k)
	if c.maxSize > 0 && size > c.maxSize {
		return
	}
	c.items[k] = c.order.PushFront(&item[K, V]{key: k, val: v, size: size})
	c.size += size
	for c.order.Len() > c.maxEntries || (c.maxSize > 0 && c.size > c.maxSize) {
		c.Remove(c.order.Back().Value.(*item[K, V]).key)
	}
}

func (c *Cache[K, V]) Remove(k K) {
	e, ok := c.items[k]
	if !ok {
		return
	}
	c.order.Remove(e)
	delete(c.items, k)
	c.size -= e.Value.(*item[K, V]).size
}
