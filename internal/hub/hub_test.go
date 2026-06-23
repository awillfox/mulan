package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBroadcastToSubscriber(t *testing.T) {
	h := New()
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	h.Broadcast("ping", "hello")

	select {
	case msg := <-ch:
		if !strings.Contains(msg, "event: ping") || !strings.Contains(msg, "data: hello") {
			t.Fatalf("bad msg: %q", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestBroadcastDropsOnFullChannel(t *testing.T) {
	h := New()
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	for i := 0; i < 100; i++ {
		h.Broadcast("e", "x")
	}
	// drain — must not block
	timeout := time.After(50 * time.Millisecond)
	for {
		select {
		case <-ch:
		case <-timeout:
			return
		}
	}
}

func TestUnsubscribeRemoves(t *testing.T) {
	h := New()
	ch := h.Subscribe()
	h.Unsubscribe(ch)

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[ch]; ok {
		t.Fatal("client still registered after Unsubscribe")
	}
}

func TestServeHTTPRequiresFlusher(t *testing.T) {
	h := New()
	rec := httptest.NewRecorder()
	w := &noFlushWriter{rec: rec}
	r := httptest.NewRequest("GET", "/events", nil)
	h.ServeHTTP(w, r)
	if rec.Code != 500 {
		t.Fatalf("status: got %d want 500", rec.Code)
	}
}

type noFlushWriter struct {
	rec *httptest.ResponseRecorder
}

func (n *noFlushWriter) Header() http.Header         { return n.rec.Header() }
func (n *noFlushWriter) Write(b []byte) (int, error) { return n.rec.Write(b) }
func (n *noFlushWriter) WriteHeader(c int)           { n.rec.WriteHeader(c) }
