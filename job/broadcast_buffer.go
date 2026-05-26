package job

import (
	"context"
	"sync"
)

// BroadcastBuffer is a concurrent buffer for multiple readers and multiple writers. It uses
// [sync.RWMutex] to allow *either*:
// - exactly one writer, no readers
// - any number of concurrent readers
// It implements [io.Writer]. [NewReader] returns a [cursor] that implements [io.Reader].
type broadcastBuffer struct {
	mu              sync.RWMutex
	newDataSignalCh chan struct{}
	data            []byte
	closed          bool
}

// Cursor represents a read position in BroadcastBuffer. It implements io.Reader. It will read a
// broadcastBuffer sequentially until it has read all the data in the buffer and the buffer is
// closed, at which point the [Read] method will return io.EOF.
type cursor struct {
	buf  *broadcastBuffer
	stop chan struct{}
	off  int
}

// Close closes the buffer and prevents further writes. Any existing readers will be able to
// continue reading the rest of the buffer. Any new readers will be able to read the entire buffer.
// Calling Close on a closed buffer will panic. A cursor at the end of a closed broadcastBuffer will
// return io.EOF on read.
func (bb *broadcastBuffer) Close() error {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bb.closed = true
	close(bb.newDataSignalCh) // Close the signal channel every time we acquire the lock
	return nil                // Implements the Closer interface
}

// Write implements the writer interface.
func (bb *broadcastBuffer) Write(p []byte) (int, error) {
	// Write
	// 1. acquires the write lock
	// 2. appends to the underlying buffer
	// 3. closes the signal channel to wake up readers
	// 4. replaces the signal channel so readers can resubscribe
	// 5. releases the write lock
	return 0, nil
}

// Read implements the reader interface. It will block when there is no data to read but the
// underlying broadcastBuffer has not been closed.
func (c *cursor) Read(p []byte) (int, error) {
	return 0, nil
}

// NewReader returns a [cursor] which will sequentially read the data contained by broadcastBuffer
// until it has read the entire buffer and broadcastBuffer is closed.
func (bb *broadcastBuffer) NewReader(ctx context.Context) *cursor {
	return nil
}
