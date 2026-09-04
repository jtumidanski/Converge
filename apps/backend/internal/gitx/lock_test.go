package gitx

import (
	"sync"
	"testing"
)

func TestLockMapSerialisesSameKeyOnly(t *testing.T) {
	var lm LockMap
	var mu sync.Mutex
	active := map[string]int{}
	maxActive := map[string]int{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		key := "a"
		if i%2 == 1 {
			key = "b"
		}
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			unlock := lm.Lock(key)
			defer unlock()
			mu.Lock()
			active[key]++
			if active[key] > maxActive[key] {
				maxActive[key] = active[key]
			}
			mu.Unlock()
			mu.Lock()
			active[key]--
			mu.Unlock()
		}(key)
	}
	wg.Wait()
	if maxActive["a"] != 1 || maxActive["b"] != 1 {
		t.Fatalf("lock not exclusive: %v", maxActive)
	}
}
