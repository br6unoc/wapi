package handler

import (
	"context"
	"wapi/internal/auth"
	"wapi/internal/hub"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

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

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	cl := hub.Global.Register(conn)
	defer hub.Global.Unregister(cl)

	// Mantém conexão aberta; descarta mensagens do cliente
	for {
		_, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}
	}
}
