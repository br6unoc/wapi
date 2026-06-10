package handler

import (
	"log"
	"net/http"
	"botwapp/internal/auth"
	"botwapp/internal/hub"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func WSHandler(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "token obrigatório")
		return
	}
	if _, err := auth.ValidateToken(token); err != nil {
		c.String(http.StatusUnauthorized, "token inválido")
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("[WS] Accept error: %v", err)
		return
	}

	cl := hub.Global.Register(conn)
	defer func() {
		hub.Global.Unregister(cl)
		log.Printf("[WS] cliente desconectado")
	}()

	log.Printf("[WS] cliente conectado")

	// Mantém conexão aberta; bloqueia até desconexão
	ctx := c.Request.Context()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}
