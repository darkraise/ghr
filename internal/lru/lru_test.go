package lru

import "testing"

func TestCacheBoundsEntriesAndSize(t *testing.T) {
	c := New[string, int](2, 10)
	c.Add("a", 1, 4)
	c.Add("b", 2, 4)
	c.Get("a")
	c.Add("c", 3, 4)
	if _, ok := c.Get("b"); ok || len(c.items) != 2 {
		t.Fatalf("b should be evicted as least recently used, %d entries", len(c.items))
	}
	c.Add("d", 4, 8)
	if _, ok := c.Get("a"); ok || len(c.items) != 1 || c.size != 8 {
		t.Fatalf("size bound: %d entries, size %d", len(c.items), c.size)
	}
	c.Add("e", 5, 11)
	if _, ok := c.Get("e"); ok || len(c.items) != 1 {
		t.Fatalf("an entry above maxSize must not be kept, %d entries", len(c.items))
	}
	if v, ok := c.Get("d"); !ok || v != 4 {
		t.Fatalf("d = %d, %v", v, ok)
	}
	c.Remove("d")
	c.Remove("d")
	if _, ok := c.Get("d"); ok || len(c.items) != 0 || c.order.Len() != 0 || c.size != 0 {
		t.Fatalf("after Remove: %d entries, %d in order, size %d", len(c.items), c.order.Len(), c.size)
	}
}

func TestCacheWithoutSizeBound(t *testing.T) {
	c := New[int, string](2, 0)
	c.Add(1, "one", 1<<40)
	c.Add(2, "two", 1<<40)
	c.Add(1, "uno", 1<<40)
	c.Add(3, "three", 1<<40)
	if _, ok := c.Get(2); ok {
		t.Fatal("2 should be evicted: adding 1 again made it the most recently used")
	}
	if v, ok := c.Get(1); !ok || v != "uno" || len(c.items) != 2 {
		t.Fatalf("1 = %q, %v, %d entries", v, ok, len(c.items))
	}
}
