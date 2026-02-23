package webrtc

import (
	"context"
	"io"

	"golang.org/x/time/rate"
)

// throttledConn wraps an io.ReadWriteCloser with token-bucket rate limiting
// for both read and write directions.
type throttledConn struct {
	inner    io.ReadWriteCloser
	readLim  *rate.Limiter // nil = unlimited
	writeLim *rate.Limiter // nil = unlimited
	ctx      context.Context
	cancel   context.CancelFunc
}

// newThrottledConn creates a rate-limited wrapper.
// bytesPerSecUp limits write throughput; bytesPerSecDown limits read throughput.
// 0 means unlimited for that direction.
func newThrottledConn(inner io.ReadWriteCloser, bytesPerSecUp, bytesPerSecDown int64) *throttledConn {
	ctx, cancel := context.WithCancel(context.Background())
	tc := &throttledConn{
		inner:  inner,
		ctx:    ctx,
		cancel: cancel,
	}

	if bytesPerSecUp > 0 {
		tc.writeLim = rate.NewLimiter(rate.Limit(bytesPerSecUp), int(bytesPerSecUp))
	}
	if bytesPerSecDown > 0 {
		tc.readLim = rate.NewLimiter(rate.Limit(bytesPerSecDown), int(bytesPerSecDown))
	}

	return tc
}

func (tc *throttledConn) Read(p []byte) (int, error) {
	n, err := tc.inner.Read(p)
	if n > 0 && tc.readLim != nil {
		if waitErr := tc.readLim.WaitN(tc.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}
	return n, err
}

func (tc *throttledConn) Write(p []byte) (int, error) {
	if tc.writeLim != nil {
		if err := tc.writeLim.WaitN(tc.ctx, len(p)); err != nil {
			return 0, err
		}
	}
	return tc.inner.Write(p)
}

func (tc *throttledConn) Close() error {
	tc.cancel()
	return tc.inner.Close()
}
