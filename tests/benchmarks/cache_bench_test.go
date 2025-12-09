package benchmarks

import (
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/VictoriaMetrics/fastcache"
	hashicorp "github.com/hashicorp/golang-lru/v2"
	"github.com/kaiachain/kaia/common/lru"
)

const (
	cacheBudgetBytes = 1 * 1024 * 1024 * 1024 // 1 GiB for comparison
	preGen           = 20000                  // pre-generated key/value pairs
)

type scenario struct {
	name       string
	keySize    int
	valSize    int
	writeEvery int // every N ops do a write; otherwise read (approx 1/N write ratio)
}

func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func makeData(sc scenario) ([][]byte, []string, [][]byte) {
	keys := make([][]byte, preGen)
	skeys := make([]string, preGen)
	vals := make([][]byte, preGen)
	for i := 0; i < preGen; i++ {
		keys[i] = generateRandomBytes(sc.keySize)
		skeys[i] = string(keys[i]) // precompute to avoid per-iteration conversion
		vals[i] = generateRandomBytes(sc.valSize)
	}
	return keys, skeys, vals
}

func lruItemBudget(valSize int) int {
	if valSize <= 0 {
		return 1
	}
	n := cacheBudgetBytes / valSize
	if n < 1 {
		return 1
	}
	return n
}

func BenchmarkCache(b *testing.B) {
	scenarios := []scenario{
		{name: "Small_Key32B_Val100B", keySize: 32, valSize: 100, writeEvery: 10},      // ~10% writes
		{name: "Mid_Key32B_Val1KB", keySize: 32, valSize: 1024, writeEvery: 10},        // ~10% writes
		{name: "Large_Key32B_Val10KB", keySize: 32, valSize: 10 * 1024, writeEvery: 5}, // ~20% writes
	}

	for _, sc := range scenarios {
		keys, skeys, vals := makeData(sc)
		itemCount := lruItemBudget(sc.valSize)

		// 1. FastCache
		b.Run(fmt.Sprintf("FastCache_%s", sc.name), func(b *testing.B) {
			c := fastcache.New(cacheBudgetBytes)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					k := keys[i%len(keys)]
					v := vals[i%len(vals)]
					if i%sc.writeEvery == 0 {
						c.Set(k, v)
					} else {
						c.Get(nil, k)
					}
					i++
				}
			})
		})

		// 2. CommonLRU
		b.Run(fmt.Sprintf("CommonLRU_%s", sc.name), func(b *testing.B) {
			c := lru.NewCache[string, []byte](itemCount)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					k := skeys[i%len(skeys)]
					v := vals[i%len(vals)]
					if i%sc.writeEvery == 0 {
						c.Add(k, v)
					} else {
						c.Get(k)
					}
					i++
				}
			})
		})

		// 3. HashicorpLRU
		b.Run(fmt.Sprintf("HashicorpLRU_%s", sc.name), func(b *testing.B) {
			c, _ := hashicorp.New[string, []byte](itemCount)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					k := skeys[i%len(skeys)]
					v := vals[i%len(vals)]
					if i%sc.writeEvery == 0 {
						c.Add(k, v)
					} else {
						c.Get(k)
					}
					i++
				}
			})
		})
	}
}

// Read-heavy HasGet vs Set for fastcache
func BenchmarkCacheHasGetFastCache(b *testing.B) {
	scenarios := []scenario{
		{name: "Val1KB_Hit90", keySize: 32, valSize: 1024, writeEvery: 10},
		{name: "Val60KB_Hit90", keySize: 32, valSize: 60 * 1024, writeEvery: 10}, // near 64KB limit
	}
	for _, sc := range scenarios {
		keys, _, vals := makeData(sc)
		b.Run(fmt.Sprintf("FastCache_HasGet_%s", sc.name), func(b *testing.B) {
			c := fastcache.New(cacheBudgetBytes)
			for i := 0; i < len(keys); i++ {
				c.Set(keys[i], vals[i])
			}
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					k := keys[i%len(keys)]
					v := vals[i%len(vals)]
					if i%sc.writeEvery == 0 {
						c.Set(k, v)
					} else {
						c.HasGet(nil, k)
					}
					i++
				}
			})
		})
	}
}

// basic-lru SizeConstrainedCache vs hashicorp SizeConstrainedCache
func BenchmarkCodeCacheSizeBounded(b *testing.B) {
	valSize := 3 * 1024 // typical contract code chunk size
	keys := make([]string, preGen)
	vals := make([][]byte, preGen)
	for i := 0; i < preGen; i++ {
		bs := generateRandomBytes(32)
		keys[i] = string(bs)
		vals[i] = generateRandomBytes(valSize)
	}

	maxSize := uint64(cacheBudgetBytes)
	type cacheOp func(k string, v []byte, write bool)

	makeBasic := func() cacheOp {
		c := lru.NewSizeConstrainedCache[string, []byte](maxSize)
		return func(k string, v []byte, write bool) {
			if write {
				c.Add(k, v)
			} else {
				c.Get(k)
			}
		}
	}
	makeHashicorp := func() cacheOp {
		c := lru.NewHashicorpSizeConstrainedCache[string, []byte](maxSize)
		return func(k string, v []byte, write bool) {
			if write {
				c.Add(k, v)
			} else {
				c.Get(k)
			}
		}
	}

	run := func(name string, maker func() cacheOp, writeEvery int) {
		b.Run(name, func(b *testing.B) {
			op := maker()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					k := keys[i%len(keys)]
					v := vals[i%len(vals)]
					op(k, v, i%writeEvery == 0)
					i++
				}
			})
		})
	}

	// Write:Read = 10%:90%
	run("BasicLRU_SizeCache_Write10", makeBasic, 10)
	run("HashicorpLRU_SizeCache_Write10", makeHashicorp, 10)
	// Write:Read = 20%:80%
	run("BasicLRU_SizeCache_Write20", makeBasic, 5)
	run("HashicorpLRU_SizeCache_Write20", makeHashicorp, 5)
}
