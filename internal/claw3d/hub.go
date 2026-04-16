package claw3d

import "sync"

// Hub tracks connected adapter clients so server-side code can broadcast.
// Mutations all go through register/unregister; readers take a read lock,
// snapshot the slice, then release — never hold the lock across a send.
type Hub struct {
	mu      sync.RWMutex
	clients map[*adapterClient]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[*adapterClient]struct{}{}} }

func (h *Hub) register(c *adapterClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unregister(c *adapterClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast sends the frame best-effort to every connected client.
// Holding no lock across send avoids deadlock if a client's send path
// circles back into the hub.
func (h *Hub) Broadcast(frame []byte) {
	h.mu.RLock()
	snap := make([]*adapterClient, 0, len(h.clients))
	for c := range h.clients {
		snap = append(snap, c)
	}
	h.mu.RUnlock()
	for _, c := range snap {
		c.send(frame)
	}
}
