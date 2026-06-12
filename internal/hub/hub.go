package hub

import (
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn      *websocket.Conn
	CompanyID string
	mu        sync.Mutex
}

func (cl *Client) Send(msg []byte) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)) //nolint
	if err := cl.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		log.Printf("[WS] send error: %v", err)
	}
}

type Hub struct {
	clients map[*Client]struct{}
	mu      sync.RWMutex
}

var Global = &Hub{clients: make(map[*Client]struct{})}

func (h *Hub) Register(conn *websocket.Conn, companyID string) *Client {
	cl := &Client{conn: conn, CompanyID: companyID}
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

// BroadcastToCompany envia apenas para clientes da mesma empresa.
func (h *Hub) BroadcastToCompany(companyID string, msg []byte) {
	h.mu.RLock()
	var targets []*Client
	for cl := range h.clients {
		if cl.CompanyID == companyID {
			targets = append(targets, cl)
		}
	}
	h.mu.RUnlock()
	for _, cl := range targets {
		go cl.Send(msg)
	}
}

// Broadcast envia para todos os clientes (usado apenas para eventos sem contexto de empresa).
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
