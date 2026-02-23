package webrtc

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// activityConn wraps an io.ReadWriteCloser and closes it after a period
// of inactivity. Any successful Read or Write resets the timer.
type activityConn struct {
	inner        io.ReadWriteCloser
	timer        *time.Timer
	timeout      time.Duration
	once         sync.Once
	lastActivity atomic.Int64 // unix nano timestamp of last I/O
}

func newActivityConn(inner io.ReadWriteCloser, timeout time.Duration) *activityConn {
	ac := &activityConn{
		inner:   inner,
		timeout: timeout,
	}
	ac.lastActivity.Store(time.Now().UnixNano())
	ac.timer = time.AfterFunc(timeout, ac.checkInactivity)
	return ac
}

// checkInactivity verifies that enough time has truly elapsed since the last
// Read/Write before closing. This eliminates the race where the timer fires
// between a successful Read/Write and the timestamp update.
func (ac *activityConn) checkInactivity() {
	elapsed := time.Since(time.Unix(0, ac.lastActivity.Load()))
	if elapsed >= ac.timeout {
		ac.Close()
		return
	}
	// Activity happened recently — reschedule for the remaining time.
	ac.timer.Reset(ac.timeout - elapsed)
}

func (ac *activityConn) Read(p []byte) (int, error) {
	n, err := ac.inner.Read(p)
	if n > 0 {
		ac.lastActivity.Store(time.Now().UnixNano())
	}
	return n, err
}

func (ac *activityConn) Write(p []byte) (int, error) {
	n, err := ac.inner.Write(p)
	if n > 0 {
		ac.lastActivity.Store(time.Now().UnixNano())
	}
	return n, err
}

func (ac *activityConn) Close() error {
	var err error
	ac.once.Do(func() {
		ac.timer.Stop()
		err = ac.inner.Close()
	})
	return err
}
