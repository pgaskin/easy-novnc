package zipfs

import "sync"

type buffer [32768]byte

var bufPool struct {
	Get  func() *buffer // Allocate a buffer
	Free func(*buffer)  // Free the buffer
}

func init() {
	var pool sync.Pool

	bufPool.Get = func() *buffer {
		b, ok := pool.Get().(*buffer)
		if !ok {
			b = new(buffer)
		}
		return b
	}

	bufPool.Free = func(b *buffer) {
		if b != nil {
			pool.Put(b)
		}
	}
}
