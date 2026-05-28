package job

import (
	"context"
	"io"
	"sync"
)

// broadcastBuffer is a concurrent buffer for multiple readers and multiple writers. It uses
// [sync.RWMutex] to allow *either*:
// - exactly one writer, no readers
// - any number of concurrent readers
// It implements [io.Writer]. [NewReader] returns a [cursor] that implements [io.Reader].
type broadcastBuffer struct {
	signalCh chan struct{}
	data     []byte
	mu       sync.RWMutex
	closed   bool
}

// cursor represents a read position in BroadcastBuffer. It implements io.Reader. It will read a
// broadcastBuffer sequentially until it has read all the data in the buffer and the buffer is
// closed, at which point the [Read] method will return io.EOF.
type cursor struct {
	ctx context.Context
	buf *broadcastBuffer
	off int
}

// newBroadcastBuffer is a convenience function for constructing a broadcastBuffer.
func newBroadcastBuffer() *broadcastBuffer {
	return &broadcastBuffer{
		signalCh: make(chan struct{}),
		// We don't really know the parameters of the workflow, so we don't set a slice capacity here.
	}
}

// close closes the buffer and prevents further writes. Any existing readers will be able to
// continue reading the rest of the buffer. Any new readers will be able to read the entire buffer.
// Calling [close] on a closed buffer will panic. A cursor at the end of a closed broadcastBuffer will
// return io.EOF on read.
func (bb *broadcastBuffer) close() {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	bb.closed = true
	// Close the signal channel to unblock readers at the write head and allow them to return EOF
	close(bb.signalCh)
	// Considered implementing the Closer interface, but decided against it as this is a non-exported
	// type and there's no internal need to return an error.
}

// newReader returns a reader which will sequentially read the data contained by broadcastBuffer
// until it has read the entire buffer and broadcastBuffer is closed.
func (bb *broadcastBuffer) newReader(ctx context.Context) io.Reader {
	return &cursor{
		buf: bb,
		ctx: ctx,
	}
}

// Write implements the writer interface.
func (bb *broadcastBuffer) Write(p []byte) (int, error) {
	bb.mu.Lock()
	defer bb.mu.Unlock()
	if bb.closed {
		panic("write to closed broadcastBuffer")
	}
	bb.data = append(bb.data, p...)
	oldSigCh := bb.signalCh
	bb.signalCh = make(chan struct{})
	close(oldSigCh)
	return len(p), nil
}

// readChunk is a convenience function that simplifies the call tree and makes it a little easier
// to manage mutexes. It takes the read lock, reads a chunk of data if available, and returns
// information about what it's read as well as updated broadcastBuffer state.
func (c *cursor) readChunk(p []byte) (int, int, bool, <-chan struct{}) {
	c.buf.mu.RLock()
	defer c.buf.mu.RUnlock()

	var n int

	available := len(c.buf.data) - c.off

	if available > 0 {
		n = copy(p, c.buf.data[c.off:])
		c.off += n
	}

	return n, available, c.buf.closed, c.buf.signalCh
}

// Read implements the reader interface. It will block when there is no data to read but the
// underlying broadcastBuffer has not been closed.
func (c *cursor) Read(p []byte) (int, error) {
	for {
		n, available, closed, wait := c.readChunk(p)
		// A reader will drain completely before checking if the buffer is closed. Therefore slow or late
		// readers should always be able to read the full buffer, even after it's closed.
		if n > 0 {
			return n, nil
		}
		// If there's still data available (but we didn't read any) we don't want to return EOF to the
		// caller.
		if available > 0 {
			return 0, nil
		}
		// We didn't read any data, there isn't any available in the buffer, and the buffer is closed
		if closed {
			return 0, io.EOF
		}
		// Wait on:
		// 1. the writer to signal that we should wake up, and
		// 2. our close signal
		select {
		case <-c.ctx.Done():
			return 0, c.ctx.Err()
		case <-wait:
		}
	}
}
