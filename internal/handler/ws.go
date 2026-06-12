package handler

import (
	"log"
	"net/http"
	"time"

	"botwapp/internal/auth"
	"botwapp/internal/hub"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	HandshakeTimeout: 10 * time.Second,
	CheckOrigin:      func(r *http.Request) bool { return true },
}

func WSHandler(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(http.StatusUnauthorized, "token obrigatório")
		return
	}
	claims, err := auth.ValidateToken(token)
	if err != nil {
		c.String(http.StatusUnauthorized, "token inválido")
		return
	}

	// Resolve company_id from user
	var companyID string
	postgres.DB.QueryRow(
		`SELECT COALESCE(company_id::text,'') FROM users WHERE id = $1`,
		claims.UserID,
	).Scan(&companyID)

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] upgrade error: %v", err)
		return
	}

	cl := hub.Global.Register(conn, companyID)
	defer func() {
		hub.Global.Unregister(cl)
		conn.Close()
		log.Printf("[WS] cliente desconectado")
	}()

	log.Printf("[WS] cliente conectado (company=%s)", companyID)

	// Ping goroutine com canal de saída para evitar leak
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cl.Send([]byte(`{"event":"ping"}`))
			case <-done:
				return
			}
		}
	}()
	defer close(done)

	// Bloqueia até desconexão
	conn.SetReadDeadline(time.Time{})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}
