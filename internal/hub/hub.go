package hub

import (
	"net/http"
	"sync"
)

// Hub broadcasts SSE messages to all connected clients.
type Hub struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func New() *Hub {
	return &Hub{clients: make(map[chan string]struct{})}
}

func (h *Hub) Subscribe() chan string {
	ch := make(chan string, 4)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) Broadcast(event, data string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	msg := "event: " + event + "\ndata: " + data + "\n\n"
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

// ServeHTTP serves the SSE stream for a single client.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := h.Subscribe()
	defer h.Unsubscribe(ch)

	// send a keep-alive comment immediately
	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write([]byte(msg))
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
