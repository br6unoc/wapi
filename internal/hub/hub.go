package hub

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (cl *Client) Send(msg []byte) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl.conn.Write(ctx, websocket.MessageText, msg) //nolint
}

type Hub struct {
	clients map[*Client]struct{}
	mu      sync.RWMutex
}

var Global = &Hub{clients: make(map[*Client]struct{})}

func (h *Hub) Register(conn *websocket.Conn) *Client {
	cl := &Client{conn: conn}
	h.mu.Lock()
	h.clients[cl] = struct{}{}
	h.mu.Unlock()
	return cl
}

func (h *Hub) Unregister(cl *Client) {
	h.mu.Lock()
	delete(h.clients, cl)
	h.mu.Unlock()
}

func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	cls := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		cls = append(cls, cl)
	}
	h.mu.RUnlock()
	for _, cl := range cls {
		go cl.Send(msg)
	}
}
