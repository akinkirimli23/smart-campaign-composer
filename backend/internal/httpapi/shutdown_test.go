package httpapi

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// streamingServer returns the API handler with an extra SSE-style route that
// sends one event and then blocks until its request context is cancelled,
// like a live progress stream with nothing new to report.
func streamingServer(t *testing.T) (*Server, <-chan error) {
	t.Helper()
	s := New(Options{DB: fakePinger{}})
	rcErr := make(chan error, 1)
	s.mux.HandleFunc("GET /test/stream", func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		// Goes through statusRecorder; only works if it implements Unwrap.
		rcErr <- rc.SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: ready\ndata: {}\n\n")
		if err := rc.Flush(); err != nil {
			return
		}
		<-r.Context().Done()
	})
	return s, rcErr
}

// openStream serves srv on a random port and returns once the stream is open.
func openStream(t *testing.T, srv *http.Server, rcErr <-chan error) <-chan error {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	resp, err := http.Get("http://" + ln.Addr().String() + "/test/stream")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	if err := <-rcErr; err != nil {
		t.Fatalf("SetWriteDeadline through statusRecorder: %v (missing Unwrap?)", err)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "event: ready\n" {
		t.Fatalf("first stream line = %q, %v", line, err)
	}
	return serveErr
}

func TestShutdownDoesNotWaitForOpenStreams(t *testing.T) {
	s, rcErr := streamingServer(t)
	srv := NewHTTPServer("", s)
	serveErr := openStream(t, srv, rcErr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v (the open stream kept the server busy)", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Shutdown took %v with one open stream, want well under a second", d)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve returned %v, want ErrServerClosed", err)
	}
}

// Control: a plain http.Server does wait for the stream, which is the bug
// NewHTTPServer exists to fix. If this ever passes Shutdown quickly, the test
// above no longer proves anything.
func TestPlainServerShutdownWaitsForOpenStreams(t *testing.T) {
	s, rcErr := streamingServer(t)
	srv := &http.Server{Handler: s}
	openStream(t, srv, rcErr)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := srv.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("plain server Shutdown = %v, want DeadlineExceeded", err)
	}
}
