package bufferpool

import (
	"io"
	"sync"
)

// BufferPool manages a pool of byte buffers for efficient I/O operations
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new buffer pool with the specified buffer size
func NewBufferPool(bufferSize int) *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() any {
				buf := make([]byte, bufferSize)
				return &buf
			},
		},
	}
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get() []byte {
	return *bp.pool.Get().(*[]byte)
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(buf []byte) {
	bp.pool.Put(&buf)
}

// CopyWithBuffer copies from src to dst using a buffer from the pool
func (bp *BufferPool) CopyWithBuffer(dst io.Writer, src io.Reader) (int64, error) {
	buf := bp.Get()
	defer bp.Put(buf)
	return io.CopyBuffer(dst, src, buf)
}
