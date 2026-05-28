package job

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ByteSize int

const (
	Byte ByteSize = 1 << (10 * iota) // 1 B
	KiB                              // 1024 B
	MiB                              // 1,048,576 B
	GiB                              // 1,073,741,824 B
)

func generateRandomByteBuffer(t *testing.T, size ByteSize) []byte {
	t.Helper()
	rng := rand.NewChaCha8([32]byte{42})

	result := make([]byte, size)
	_, err := rng.Read(result)
	require.NoError(t, err)
	return result
}

func TestSmallWrite(t *testing.T) {
	bb := newBroadcastBuffer()

	expected := []byte("some small amount of data that should be recovered correctly")

	n, err := bb.Write(expected)

	require.NoError(t, err)
	assert.Equal(t, len(expected), n)
	assert.Equal(t, expected, bb.data)
}

func TestLargeWrite(t *testing.T) {
	bb := newBroadcastBuffer()
	expected := generateRandomByteBuffer(t, 1*GiB)

	n, err := bb.Write(expected)

	require.NoError(t, err)
	assert.Equal(t, len(expected), n)
	assert.Equal(t, expected, bb.data)
}

func TestMultipleWrite(t *testing.T) {
	bb := newBroadcastBuffer()
	rng := rand.NewChaCha8([32]byte{42})

	size := 4 * MiB
	expected := make([]byte, size)
	_, err := rng.Read(expected)
	require.NoError(t, err)

	for _, v := range expected {
		n, err := bb.Write([]byte{v})
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	}

	assert.Equal(t, expected, bb.data)
}

func TestWriteClosedBuffer(t *testing.T) {
	bb := newBroadcastBuffer()
	bb.close()
	_, err := bb.Write([]byte{})
	assert.ErrorIs(t, err, ErrWriteToClosedBroadcastBuffer)
}

func TestReadEmptyAndClose(t *testing.T) {
	bb := newBroadcastBuffer()
	reader := bb.newReader(t.Context())

	expected := make([]byte, 0)
	actual := make([]byte, 0)
	var readErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		actual, readErr = io.ReadAll(reader)
	}()

	bb.close()

	<-done

	assert.NoError(t, readErr)
	assert.Equal(t, expected, actual)
}

func TestReadBeforeWriteAndClose(t *testing.T) {
	bb := newBroadcastBuffer()
	reader := bb.newReader(t.Context())

	expected := []byte("some small amount of data that should be recovered correctly")
	actual := make([]byte, 0)
	var readErr error
	done := make(chan struct{})

	go func() {
		defer close(done)
		actual, readErr = io.ReadAll(reader)
	}()

	_, err := bb.Write(expected)
	require.NoError(t, err)
	bb.close()

	<-done

	require.NoError(t, readErr)
	assert.Equal(t, expected, actual)
}

func TestWriteBeforeReadAndClose(t *testing.T) {
	bb := newBroadcastBuffer()
	reader := bb.newReader(t.Context())

	expected := []byte("some small amount of data that should be recovered correctly")
	var actual []byte

	_, err := bb.Write(expected)
	require.NoError(t, err)
	done := make(chan struct{})

	go func() {
		defer close(done)
		var readErr error
		actual, readErr = io.ReadAll(reader)
		assert.NoError(t, readErr)
	}()

	bb.close()
	<-done

	assert.Equal(t, expected, actual)
}

func TestWriteAndCloseBeforeRead(t *testing.T) {
	bb := newBroadcastBuffer()
	reader := bb.newReader(t.Context())

	expected := []byte("some small amount of data that should be recovered correctly")

	_, err := bb.Write(expected)
	require.NoError(t, err)
	bb.close()

	actual, readErr := io.ReadAll(reader)
	require.NoError(t, readErr)

	assert.Equal(t, expected, actual)
}

func TestWriteAndCloseBeforeReads(t *testing.T) {
	bb := newBroadcastBuffer()
	reader0 := bb.newReader(t.Context())
	reader1 := bb.newReader(t.Context())

	expected := []byte("some small amount of data that should be recovered correctly")

	_, err := bb.Write(expected)
	require.NoError(t, err)
	bb.close()

	actual0, readErr0 := io.ReadAll(reader0)
	require.NoError(t, readErr0)
	actual1, readErr1 := io.ReadAll(reader1)
	require.NoError(t, readErr1)

	assert.Equal(t, expected, actual0)
	assert.Equal(t, expected, actual1)
}

func TestZeroLengthReadWithDataAvailable(t *testing.T) {
	bb := newBroadcastBuffer()
	reader := bb.newReader(t.Context())

	_, err := bb.Write([]byte("hello"))
	require.NoError(t, err)
	bb.close()

	n, err := reader.Read([]byte{})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestZeroLengthReadAfterDrainAndClose(t *testing.T) {
	bb := newBroadcastBuffer()
	reader := bb.newReader(t.Context())

	data := []byte("hello")
	_, err := bb.Write(data)
	require.NoError(t, err)
	bb.close()

	buf := make([]byte, len(data))
	_, err = reader.Read(buf)
	require.NoError(t, err)

	n, err := reader.Read([]byte{})
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 0, n)
}

func TestReaderLeavesBeforeWriteFinished(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	bb := newBroadcastBuffer()

	expected := []byte("some small amount of data that should be recovered correctly")
	var actual []byte

	_, err := bb.Write(expected)
	require.NoError(t, err)

	done := make(chan struct{})
	reader := bb.newReader(ctx)
	go func() {
		defer close(done)
		var readErr error
		actual, readErr = io.ReadAll(reader)
		assert.ErrorIs(t, readErr, context.Canceled)
	}()
	cancel()
	<-done

	assert.Equal(t, expected, actual)
}

func TestMultipleReadersReceiveSameData(t *testing.T) {
	bb := newBroadcastBuffer()
	reader0 := bb.newReader(t.Context())
	reader1 := bb.newReader(t.Context())

	expected := []byte("some small amount of data that should be recovered correctly")
	var actual0, actual1 []byte

	_, err := bb.Write(expected)
	require.NoError(t, err)
	var wg sync.WaitGroup

	wg.Go(func() {
		var readErr0 error
		actual0, readErr0 = io.ReadAll(reader0)
		assert.NoError(t, readErr0)
	})

	wg.Go(func() {
		var readErr1 error
		actual1, readErr1 = io.ReadAll(reader1)
		assert.NoError(t, readErr1)
	})

	bb.close()
	wg.Wait()

	assert.Equal(t, expected, actual0)
	assert.Equal(t, expected, actual1)
}

func TestReaderSuspendsBeforeWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bb := newBroadcastBuffer()
		reader := bb.newReader(t.Context())

		expected := make([]byte, 0)
		actual := make([]byte, 0)
		var readErr error
		done := make(chan struct{})

		go func() {
			defer close(done)
			actual, readErr = io.ReadAll(reader)
		}()

		synctest.Wait()

		bb.close()

		<-done

		require.NoError(t, readErr)
		assert.Equal(t, expected, actual)
	})
}

func TestReadersSuspendBeforeWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bb := newBroadcastBuffer()
		reader0 := bb.newReader(t.Context())
		reader1 := bb.newReader(t.Context())
		reader2 := bb.newReader(t.Context())

		expected := make([]byte, 0)
		var actual0, actual1, actual2 []byte
		var readErr0, readErr1, readErr2 error

		var wg sync.WaitGroup
		wg.Go(func() {
			actual0, readErr0 = io.ReadAll(reader0)
		})
		wg.Go(func() {
			actual1, readErr1 = io.ReadAll(reader1)
		})
		wg.Go(func() {
			actual2, readErr2 = io.ReadAll(reader2)
		})

		synctest.Wait()

		bb.close()

		wg.Wait()

		require.NoError(t, readErr0)
		assert.Equal(t, expected, actual0)
		require.NoError(t, readErr1)
		assert.Equal(t, expected, actual1)
		require.NoError(t, readErr2)
		assert.Equal(t, expected, actual2)
	})
}

func TestReaderSuspendsAtWriteHead(t *testing.T) {
	expected := []byte("some small amount of data that should be recovered correctly")
	for readBufSize := 1; readBufSize <= len(expected); readBufSize++ {
		synctest.Test(t, func(t *testing.T) {
			bb := newBroadcastBuffer()
			reader := bb.newReader(t.Context())

			actual := make([]byte, 0)

			n, err := bb.Write(expected)
			require.NoError(t, err)
			require.Equal(t, len(expected), n)

			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					p := make([]byte, readBufSize)
					n, readErr := reader.Read(p)
					if n > 0 {
						actual = append(actual, p[:n]...)
					}
					if errors.Is(readErr, io.EOF) {
						return
					}
					assert.NoError(t, readErr)
				}
			}()

			synctest.Wait()
			// To check that the reader actually suspended at the write head instead of before, we verify
			// that the buffer it's read before it is suspended equals the whole written buffer.
			require.Equal(t, expected, actual)
			bb.close()
			<-done
			assert.Equal(t, expected, actual)
		})
	}
}

func TestReadersSuspendAtWriteHead(t *testing.T) {
	expected := []byte("some small amount of data that should be recovered correctly")
	for readBufSize := 1; readBufSize <= len(expected); readBufSize++ {
		synctest.Test(t, func(t *testing.T) {
			bb := newBroadcastBuffer()
			reader0 := bb.newReader(t.Context())
			reader1 := bb.newReader(t.Context())

			var actual0, actual1 []byte

			n, err := bb.Write(expected)
			require.NoError(t, err)
			require.Equal(t, len(expected), n)

			var wg sync.WaitGroup

			wg.Go(func() {
				for {
					p := make([]byte, readBufSize)
					n, readErr0 := reader0.Read(p)
					if n > 0 {
						actual0 = append(actual0, p[:n]...)
					}
					if errors.Is(readErr0, io.EOF) {
						return
					}
					assert.NoError(t, readErr0)
				}
			})

			wg.Go(func() {
				for {
					p := make([]byte, readBufSize)
					n, readErr1 := reader1.Read(p)
					if n > 0 {
						actual1 = append(actual1, p[:n]...)
					}
					if errors.Is(readErr1, io.EOF) {
						return
					}
					assert.NoError(t, readErr1)
				}
			})

			synctest.Wait()
			require.Equal(t, expected, actual0)
			require.Equal(t, expected, actual1)
			bb.close()

			wg.Wait()

			assert.Equal(t, expected, actual0)
			assert.Equal(t, expected, actual1)
		})
	}
}

func TestLargeMultipleReaders(t *testing.T) {
	type testReaderInfo struct {
		cur io.Reader
		buf []byte
	}
	createReaderInfo := func(bb *broadcastBuffer) *testReaderInfo {
		return &testReaderInfo{
			cur: bb.newReader(t.Context()),
			buf: nil,
		}
	}
	bb := newBroadcastBuffer()
	readers := make([]*testReaderInfo, 20)
	for i := range readers {
		readers[i] = createReaderInfo(bb)
	}
	expected := generateRandomByteBuffer(t, 128*MiB)

	_, err := bb.Write(expected)
	assert.NoError(t, err)

	var wg sync.WaitGroup
	for _, r := range readers {
		wg.Go(func() {
			var err error
			r.buf, err = io.ReadAll(r.cur)
			assert.NoError(t, err)
		})
	}
	bb.close()
	wg.Wait()

	for _, r := range readers {
		assert.Equal(t, expected, r.buf)
	}
}

func TestInterleavedReadWriteSequence(t *testing.T) {
	bb := newBroadcastBuffer()

	var actual0, actual1 []byte

	expectedArr := [][]byte{
		[]byte("some small amount "),
		[]byte("of data that should "),
		[]byte("be recovered correctly"),
	}
	expected := append(append(expectedArr[0], expectedArr[1]...), expectedArr[2]...)

	// reader0 will begin reading before the first write
	reader0 := bb.newReader(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p := make([]byte, 100)
		n, err := reader0.Read(p)
		assert.NoError(t, err)
		actual0 = append(actual0, p[:n]...)
	}()

	_, err := bb.Write(expectedArr[0])
	require.NoError(t, err)
	<-done
	assert.Equal(t, expectedArr[0], actual0)

	// reader1 is intentionally created later, and will read one byte
	reader1 := bb.newReader(t.Context())
	reader1FirstReadSize := 1
	done = make(chan struct{})
	go func() {
		defer close(done)
		p := make([]byte, reader1FirstReadSize)
		n, reader1Err := reader1.Read(p)
		assert.NoError(t, reader1Err)
		assert.Equal(t, reader1FirstReadSize, n)
		assert.Equal(t, expectedArr[0][:reader1FirstReadSize], p)
		actual1 = append(actual1, p[:n]...)
	}()
	<-done

	// reader0 continues reading
	done = make(chan struct{})
	go func() {
		defer close(done)
		p := make([]byte, 100)
		n, reader0Err := reader0.Read(p)
		assert.NoError(t, reader0Err)
		actual0 = append(actual0, p[:n]...)
	}()
	_, err = bb.Write(expectedArr[1])
	require.NoError(t, err)
	<-done
	assert.Equal(t, append(expectedArr[0], expectedArr[1]...), actual0)

	// we finish writing, then finish reading
	_, err = bb.Write(expectedArr[2])
	require.NoError(t, err)
	assert.Equal(t, expected, bb.data)

	// reader1 reads the rest of the data, overtaking reader0
	done = make(chan struct{})
	go func() {
		defer close(done)
		m := len(expected) - reader1FirstReadSize
		p := make([]byte, m)
		n, reader1Err := reader1.Read(p)
		assert.NoError(t, reader1Err)
		assert.Equal(t, m, n)
		assert.Equal(t, expected[reader1FirstReadSize:], p)
		actual1 = append(actual1, p[:n]...)
	}()
	<-done

	// Close the writer
	bb.close()

	// reader0 finishes reading after close, a couple of bytes at a time
	done = make(chan struct{})
	go func() {
		defer close(done)
		for {
			p := make([]byte, 2)
			n, err := reader0.Read(p)
			if errors.Is(err, io.EOF) {
				return
			}
			assert.NoError(t, err)
			actual0 = append(actual0, p[:n]...)
		}
	}()
	<-done

	assert.Equal(t, expected, actual0)
	assert.Equal(t, expected, actual1)
}
