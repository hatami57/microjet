package middleware

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout bounds how long a request may take. It runs the handler chain with a
// context cancelled after d and, if the deadline fires first, flushes a 503
// Service Unavailable to the client immediately. Opt-in, like CORS; a
// non-positive d disables it. Register it after gin.Recovery (the default stack
// does) so a handler panic still produces a clean 500.
//
// The response is buffered until the handler completes, so a late-finishing
// handler cannot corrupt the 503 already sent — which makes Timeout incompatible
// with streaming responses (SSE, chunked flushing): those are held until the
// handler returns.
//
// Caveat: on expiry only the request context is cancelled; the handler is not
// forcibly stopped and the request goroutine stays blocked in it until it
// returns (ideally by observing ctx.Done()). A handler that ignores its context
// will not free that goroutine until it finishes on its own — but the client has
// already received the 503.
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if d <= 0 {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		c.Request = c.Request.WithContext(ctx)

		tw := &timeoutWriter{ResponseWriter: c.Writer, code: http.StatusOK}
		c.Writer = tw

		// A watcher goroutine sends the 503 the instant the deadline fires, even
		// while the handler is still blocked on this goroutine. It only ever touches
		// the mutex-guarded writer — never the gin.Context, which stays confined to
		// this goroutine — so there is no concurrent access to gin's request state.
		done := make(chan struct{})
		var watcher sync.WaitGroup
		watcher.Go(func() {
			select {
			case <-ctx.Done():
				// Only a real deadline reaches here: cancel() is called after the
				// watcher is drained below, so the watcher never sees a manual cancel.
				tw.writeTimeout()
			case <-done:
			}
		})

		panicked := true
		defer func() {
			// Retire the watcher before deciding the response, so it can no longer
			// write to the connection, then release the context.
			close(done)
			watcher.Wait()
			cancel()
			if panicked {
				// Handler panicked: drop any partial buffered output and let the
				// upstream gin.Recovery write its 500 straight through (a no-op if the
				// deadline already fired and we responded 503).
				tw.enablePassthrough()
				return
			}
			tw.commit()
		}()

		c.Next()
		panicked = false
	}
}

// writerState is the lifecycle of a timeoutWriter: it buffers output until the
// request either completes (commit → passthrough) or times out (writeTimeout →
// timedOut, discarding further writes from the still-running handler).
type writerState int

const (
	stBuffering writerState = iota
	stPassthrough
	stTimedOut
)

// timeoutWriter buffers the handler's response so the Timeout middleware can
// decide, once the handler finishes or the deadline fires, whether to flush it
// or replace it with a 503. Its methods are guarded because the watcher goroutine
// may write the 503 concurrently with the handler still writing into the buffer.
type timeoutWriter struct {
	gin.ResponseWriter

	mu    sync.Mutex
	buf   bytes.Buffer
	code  int
	state writerState
}

func (w *timeoutWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch w.state {
	case stTimedOut:
	case stPassthrough:
		w.ResponseWriter.WriteHeader(code)
	default:
		w.code = code
	}
}

func (w *timeoutWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch w.state {
	case stTimedOut:
		// Discard: the 503 has already been written to the client.
		return len(b), nil
	case stPassthrough:
		return w.ResponseWriter.Write(b)
	default:
		return w.buf.Write(b)
	}
}

func (w *timeoutWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

// WriteHeaderNow is called by gin to flush headers eagerly; while buffering we
// defer the real write to commit/writeTimeout, so it is a no-op until passthrough.
func (w *timeoutWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == stPassthrough {
		w.ResponseWriter.WriteHeaderNow()
	}
}

// Flush is suppressed while buffering; Timeout does not support streaming.
func (w *timeoutWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state == stPassthrough {
		w.ResponseWriter.Flush()
	}
}

func (w *timeoutWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.code
}

func (w *timeoutWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state != stBuffering
}

// commit flushes the buffered response and switches to passthrough. Called once
// the handler returns cleanly; a no-op if the deadline already responded 503.
func (w *timeoutWriter) commit() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != stBuffering {
		return
	}
	w.state = stPassthrough
	w.ResponseWriter.WriteHeader(w.code)
	if w.buf.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.buf.Bytes())
	}
}

// writeTimeout sends the 503 and switches to timedOut so the still-running
// handler's later writes are discarded. A no-op if the handler already committed.
func (w *timeoutWriter) writeTimeout() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != stBuffering {
		return
	}
	w.state = stTimedOut
	w.code = http.StatusServiceUnavailable
	h := w.ResponseWriter.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	w.ResponseWriter.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.ResponseWriter.Write([]byte(`{"error":"timeout","message":"request exceeded the configured timeout"}`))
	// Push the 503 to the client now; the request goroutine may still be blocked
	// in the handler, so returning from ServeHTTP will not flush it for us.
	w.ResponseWriter.Flush()
}

// enablePassthrough discards any buffered output and sends subsequent writes
// straight to the client. Used on panic so upstream recovery can respond; a
// no-op once we have already timed out and responded.
func (w *timeoutWriter) enablePassthrough() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.state != stBuffering {
		return
	}
	w.buf.Reset()
	w.state = stPassthrough
}
