package handler

import (
	"log"
	"net/http"
	"time"
	"botwapp/internal/auth"
	"botwapp/internal/hub"

	"github.com/gorilla/websocket"
	"github.com/gin-gonic/gin"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func WSHandler(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(401, "token obrigatório")
		return
	}
	if _, err := auth.ValidateToken(token); err != nil {
		c.String(401, "token inválido")
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return
	}

	cl := hub.Global.Register(conn)
	defer func() {
		hub.Global.Unregister(cl)
		log.Printf("[WS] cliente desconectado, restam=%d", hub.Global.Count())
	}()

	log.Printf("[WS] cliente conectado, total=%d", hub.Global.Count())

	// writePump em goroutine separada
	go cl.WritePump()

	// readPump: mantém conexão aberta e detecta desconexão
	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
