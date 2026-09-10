package gitx

import "sync"

// LockMap hands out one mutex per key (mirror path).
type LockMap struct{ m sync.Map }

// Lock blocks until the key's mutex is held and returns the unlock func.
func (l *LockMap) Lock(key string) func() {
	v, _ := l.m.LoadOrStore(key, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
