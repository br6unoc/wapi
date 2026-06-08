package handler

import (
	"net/http"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
)

func ListConversations(c *gin.Context) {
	rows, err := postgres.DB.Query(`
		SELECT
			c.id AS contact_id,
			c.phone,
			CASE WHEN c.name != '' THEN c.name ELSE c.phone END AS name,
			i.id AS instance_id,
			i.name AS instance_name,
			m.content AS last_message,
			m.direction AS last_direction,
			m.type AS last_type,
			m.created_at AS last_message_at
		FROM contacts c
		JOIN instances i ON i.id = c.instance_id
		LEFT JOIN LATERAL (
			SELECT content, direction, type, created_at
			FROM messages
			WHERE contact_id = c.id
			ORDER BY created_at DESC
			LIMIT 1
		) m ON true
		WHERE m.created_at IS NOT NULL
		ORDER BY m.created_at DESC
		LIMIT 50
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Conv struct {
		ContactID     string `json:"contact_id"`
		Phone         string `json:"phone"`
		Name          string `json:"name"`
		InstanceID    string `json:"instance_id"`
		InstanceName  string `json:"instance_name"`
		LastMessage   string `json:"last_message"`
		LastDirection string `json:"last_direction"`
		LastType      string `json:"last_type"`
		LastMessageAt string `json:"last_message_at"`
	}

	convs := make([]Conv, 0)
	for rows.Next() {
		var conv Conv
		if err := rows.Scan(
			&conv.ContactID, &conv.Phone, &conv.Name,
			&conv.InstanceID, &conv.InstanceName,
			&conv.LastMessage, &conv.LastDirection, &conv.LastType,
			&conv.LastMessageAt,
		); err != nil {
			continue
		}
		convs = append(convs, conv)
	}
	c.JSON(http.StatusOK, convs)
}

func GetMessages(c *gin.Context) {
	instanceName := c.Param("name")
	phone := c.Param("phone")

	rows, err := postgres.DB.Query(`
		SELECT m.id, m.direction, m.content, m.type, m.created_at
		FROM messages m
		JOIN contacts c ON c.id = m.contact_id
		JOIN instances i ON i.id = m.instance_id
		WHERE i.name = $1 AND c.phone = $2
		ORDER BY m.created_at ASC
		LIMIT 200
	`, instanceName, phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Msg struct {
		ID        string `json:"id"`
		Direction string `json:"direction"`
		Content   string `json:"content"`
		Type      string `json:"type"`
		CreatedAt string `json:"created_at"`
	}

	msgs := make([]Msg, 0)
	for rows.Next() {
		var msg Msg
		if err := rows.Scan(&msg.ID, &msg.Direction, &msg.Content, &msg.Type, &msg.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, msg)
	}
	c.JSON(http.StatusOK, msgs)
}
