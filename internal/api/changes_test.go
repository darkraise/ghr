package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/changes"
)

type topicSource struct {
	mu sync.Mutex
	fp map[string]uint64
}

func (s *topicSource) get() map[string]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.fp)
}

func (s *topicSource) set(topic string, v uint64) {
	s.mu.Lock()
	s.fp[topic] = v
	s.mu.Unlock()
}

func liveServer(t *testing.T) (*httptest.Server, *fakeBackend, *topicSource) {
	t.Helper()
	src := &topicSource{fp: map[string]uint64{"config": 1, "status": 1}}
	hub := changes.New(changes.Options{Tick: 10 * time.Millisecond})
	hub.SetSources(src.get, nil)
	t.Cleanup(hub.Close)
	b := &fakeBackend{hub: hub}
	srv := httptest.NewServer(NewServer(b))
	t.Cleanup(srv.Close)
	return srv, b, src
}

func openChanges(t *testing.T, srv *httptest.Server) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})
	return resp, bufio.NewReader(resp.Body), cancel
}

// frame reads the lines up to the next blank line.
func frame(t *testing.T, rd *bufio.Reader) string {
	t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var sb strings.Builder
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				ch <- result{sb.String(), err}
				return
			}
			if line == "\n" {
				ch <- result{sb.String(), nil}
				return
			}
			sb.WriteString(line)
		}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("reading a frame: %v (read %q)", r.err, r.s)
		}
		return r.s
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5 s")
	}
	return ""
}

func TestChangesStreamsHelloAndChanges(t *testing.T) {
	srv, _, src := liveServer(t)
	resp, rd, _ := openChanges(t, srv)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for k, want := range map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-store", "X-Accel-Buffering": "no"} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if f := frame(t, rd); f != "event: hello\ndata: {}\n" {
		t.Fatalf("first frame %q", f)
	}
	src.set("status", 2)
	if f := frame(t, rd); f != "event: change\ndata: {\"topics\":[\"status\"]}\n" {
		t.Fatalf("change frame %q", f)
	}
}

func TestChangesPingsWhenIdle(t *testing.T) {
	old := heartbeat
	heartbeat = 50 * time.Millisecond
	t.Cleanup(func() { heartbeat = old })
	srv, _, _ := liveServer(t)
	_, rd, _ := openChanges(t, srv)
	frame(t, rd)
	if f := frame(t, rd); f != ": ping\n" {
		t.Fatalf("idle frame %q", f)
	}
}

func TestChangesRefusals(t *testing.T) {
	notReady := changes.New(changes.Options{})
	full := changes.New(changes.Options{})
	full.SetSources(func() map[string]uint64 { return map[string]uint64{} }, nil)
	for range changes.MaxSubscribers {
		if _, err := full.Subscribe(); err != nil {
			t.Fatal(err)
		}
	}
	closed := changes.New(changes.Options{})
	closed.Close()
	t.Cleanup(full.Close)
	for _, tc := range []struct {
		name string
		hub  *changes.Hub
		want string
	}{
		{"no hub", nil, "not ready"},
		{"no sources", notReady, "not ready"},
		{"full", full, "too many live connections"},
		{"closed", closed, "shutting down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(NewServer(&fakeBackend{hub: tc.hub}))
			defer srv.Close()
			resp, err := http.Get(srv.URL + "/changes")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body map[string]string
			json.NewDecoder(resp.Body).Decode(&body)
			if resp.StatusCode != http.StatusServiceUnavailable || body["error"] != tc.want {
				t.Fatalf("%d %v", resp.StatusCode, body)
			}
		})
	}
}

func TestChangesEndsWhenTheClientLeaves(t *testing.T) {
	srv, b, _ := liveServer(t)
	_, rd, cancel := openChanges(t, srv)
	frame(t, rd)
	cancel()
	b.subMu.Lock()
	sub := b.subs[0]
	b.subMu.Unlock()
	select {
	case _, ok := <-sub.C:
		if ok {
			t.Fatal("a set arrived instead of the subscription closing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription outlived its client")
	}
}

func TestChangesEndsWhenTheHubCloses(t *testing.T) {
	srv, b, _ := liveServer(t)
	_, rd, _ := openChanges(t, srv)
	frame(t, rd)
	b.hub.Close()
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(rd)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the stream ended with %v, want a clean end", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream outlived the hub")
	}
}

// plainWriter has Flush but no write deadline, like a test recorder.
type plainWriter struct {
	mu  sync.Mutex
	h   http.Header
	buf strings.Builder
}

func (p *plainWriter) Header() http.Header { return p.h }
func (p *plainWriter) WriteHeader(int)     {}
func (p *plainWriter) Flush()              {}

func (p *plainWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Write(b)
}

func (p *plainWriter) body() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

func TestChangesStreamWithoutWriteDeadlines(t *testing.T) {
	src := &topicSource{fp: map[string]uint64{"status": 1}}
	hub := changes.New(changes.Options{Tick: 10 * time.Millisecond})
	hub.SetSources(src.get, nil)
	t.Cleanup(hub.Close)
	ctx, cancel := context.WithCancel(context.Background())
	w := &plainWriter{h: http.Header{}}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/changes", nil)
	done := make(chan struct{})
	go func() {
		NewServer(&fakeBackend{hub: hub}).ServeHTTP(w, req)
		close(done)
	}()
	waitBody := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(w.body(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("body %q never held %q", w.body(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitBody("event: hello\n")
	src.set("status", 2)
	waitBody("event: change\n")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return after its request ended")
	}
}
