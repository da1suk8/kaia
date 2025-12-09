package lru

import (
	"math"
	"sync"

	hlru "github.com/hashicorp/golang-lru/v2"
)

type HashicorpSizeConstrainedCache[K comparable, V blobType] struct {
	size    uint64
	maxSize uint64
	lru     *hlru.Cache[K, V]
	lock    sync.Mutex
}

func NewHashicorpSizeConstrainedCache[K comparable, V blobType](maxSize uint64) *HashicorpSizeConstrainedCache[K, V] {
	lruCache, _ := hlru.New[K, V](math.MaxInt) // errors only on non-positive sizes
	return &HashicorpSizeConstrainedCache[K, V]{
		size:    0,
		maxSize: maxSize,
		lru:     lruCache,
	}
}

func (c *HashicorpSizeConstrainedCache[K, V]) Add(key K, value V) (evicted bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if !c.lru.Contains(key) {
		targetSize := c.size + uint64(len(value))
		for targetSize > c.maxSize {
			evicted = true
			_, v, ok := c.lru.RemoveOldest()
			if !ok {
				break
			}
			valSize := uint64(len(v))
			if targetSize >= valSize {
				targetSize -= valSize
			} else {
				targetSize = 0
			}
		}
		c.size = targetSize
	}

	c.lru.Add(key, value)
	return evicted
}

func (c *HashicorpSizeConstrainedCache[K, V]) Get(key K) (V, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.lru.Get(key)
}

func (c *HashicorpSizeConstrainedCache[K, V]) DeleteCode(key K) bool {
	c.lock.Lock()
	defer c.lock.Unlock()

	value, ok := c.lru.Peek(key)
	if !ok {
		return false
	}

	valSize := uint64(len(value))
	if c.size >= valSize {
		c.size -= valSize
	} else {
		c.size = 0
	}

	return c.lru.Remove(key)
}
