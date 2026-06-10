package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"botwapp/internal/hub"
	"botwapp/internal/instance"
	"botwapp/internal/service"
	"botwapp/internal/transcriber"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListConversations(c *gin.Context) {
	rows, err := postgres.DB.Query(`
		SELECT
			c.id AS contact_id,
			c.phone,
			CASE WHEN c.name != '' THEN c.name ELSE c.phone END AS name,
			c.unread_count,
			i.id AS instance_id,
			i.name AS instance_name,
			m.content AS last_message,
			m.direction AS last_direction,
			m.type AS last_type,
			m.created_at AS last_message_at,
			c.agent_mode
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
		LIMIT 100
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
		UnreadCount   int    `json:"unread_count"`
		InstanceID    string `json:"instance_id"`
		InstanceName  string `json:"instance_name"`
		LastMessage   string `json:"last_message"`
		LastDirection string `json:"last_direction"`
		LastType      string `json:"last_type"`
		LastMessageAt string `json:"last_message_at"`
		AgentMode     bool   `json:"agent_mode"`
	}

	convs := make([]Conv, 0)
	for rows.Next() {
		var conv Conv
		if err := rows.Scan(
			&conv.ContactID, &conv.Phone, &conv.Name, &conv.UnreadCount,
			&conv.InstanceID, &conv.InstanceName,
			&conv.LastMessage, &conv.LastDirection, &conv.LastType,
			&conv.LastMessageAt, &conv.AgentMode,
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
		SELECT m.id, m.direction, m.content, m.type, m.media_path, m.media_name, m.created_at
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
		MediaPath string `json:"media_path"`
		MediaName string `json:"media_name"`
		CreatedAt string `json:"created_at"`
	}

	msgs := make([]Msg, 0)
	for rows.Next() {
		var msg Msg
		if err := rows.Scan(&msg.ID, &msg.Direction, &msg.Content, &msg.Type, &msg.MediaPath, &msg.MediaName, &msg.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, msg)
	}
	c.JSON(http.StatusOK, msgs)
}

// SendMediaFromUI envia foto ou vídeo via upload multipart (JWT auth).
func SendMediaFromUI(c *gin.Context) {
	instanceName := c.Param("name")
	phone := c.Param("phone")

	inst, ok := instance.Global.GetByName(instanceName)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "arquivo obrigatório"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao ler arquivo"})
		return
	}

	caption := c.Request.FormValue("caption")
	mimetype := header.Header.Get("Content-Type")
	if mimetype == "" {
		mimetype = http.DetectContentType(data)
	}
	filename := header.Filename

	isImage := strings.HasPrefix(mimetype, "image/")
	isVideo := strings.HasPrefix(mimetype, "video/")
	isAudio := strings.HasPrefix(mimetype, "audio/")

	var msgType string
	switch {
	case isImage:
		msgType = "image"
	case isVideo:
		msgType = "video"
	case isAudio:
		msgType = "audio"
	default:
		msgType = "document"
	}

	// Converte áudio para OGG/Opus (necessário para reprodução em iOS e Android)
	if isAudio {
		converted, err := service.ConvertToOpus(data)
		if err != nil {
			log.Printf("[SEND_MEDIA] Falha ao converter áudio para Opus: %v", err)
		} else {
			data = converted
			mimetype = "audio/ogg; codecs=opus"
			filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".ogg"
		}
	}

	number := strings.TrimPrefix(phone, "+")
	number = strings.ReplaceAll(number, " ", "")

	// Salva na pasta de mídia
	ext := filepath.Ext(filename)
	if ext == "" {
		switch msgType {
		case "image":
			ext = ".jpg"
		case "video":
			ext = ".mp4"
		case "audio":
			ext = ".ogg"
		default:
			ext = ".bin"
		}
	}
	mediaID := uuid.New().String()
	mediaDir := fmt.Sprintf("/app/media/%ss", msgType)
	os.MkdirAll(mediaDir, 0755)
	fsPath := fmt.Sprintf("%s/%s%s", mediaDir, mediaID, ext)
	webPath := fmt.Sprintf("/media/%ss/%s%s", msgType, mediaID, ext)
	if err := os.WriteFile(fsPath, data, 0644); err != nil {
		log.Printf("[SEND_MEDIA] Erro ao salvar arquivo: %v", err)
		webPath = ""
	}

	// Salva no DB antes de enviar (optimistic)
	var contactID string
	if err := postgres.DB.QueryRow(
		`INSERT INTO contacts (instance_id, phone, name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (instance_id, phone) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
		inst.ID, number, number,
	).Scan(&contactID); err != nil {
		log.Printf("[DB] Erro ao upsert contact: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar contato"})
		return
	}

	content := caption
	if content == "" {
		content = "[" + msgType + "]"
	}
	mediaName := filename

	msgID := uuid.New().String()
	var createdAt string
	if err := postgres.DB.QueryRow(
		`INSERT INTO messages (id, instance_id, contact_id, direction, content, type, media_path, media_name)
		 VALUES ($1, $2, $3, 'out', $4, $5, $6, $7)
		 RETURNING created_at`,
		msgID, inst.ID, contactID, content, msgType, webPath, mediaName,
	).Scan(&createdAt); err != nil {
		log.Printf("[DB] Erro ao salvar mensagem de mídia: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar mensagem"})
		return
	}

	// Broadcast imediato para o UI
	payload := map[string]interface{}{
		"event": "new_message",
		"data": map[string]interface{}{
			"id":            msgID,
			"instance_id":   inst.ID,
			"instance_name": inst.Name,
			"contact_id":    contactID,
			"phone":         number,
			"name":          number,
			"direction":     "out",
			"content":       content,
			"type":          msgType,
			"media_path":    webPath,
			"media_name":    mediaName,
			"created_at":    createdAt,
		},
	}
	jsonBytes, _ := json.Marshal(payload)
	hub.Global.Broadcast(jsonBytes)

	// Desativa agente quando humano envia mensagem
	postgres.DB.Exec(`UPDATE contacts SET agent_mode = FALSE WHERE id = $1`, contactID)
	mediaAgentPayload, _ := json.Marshal(map[string]interface{}{
		"type":       "agent_mode_changed",
		"contact_id": contactID,
		"agent_mode": false,
	})
	hub.Global.Broadcast(mediaAgentPayload)

	c.JSON(http.StatusOK, gin.H{"ok": true, "id": msgID})

	// Envia para o WhatsApp em background
	go func() {
		if err := service.SendMedia(inst, number, data, mimetype, filename, caption, isAudio); err != nil {
			log.Printf("[SEND_MEDIA] Erro ao enviar para WhatsApp: %v", err)
		}
	}()
}

func TranscribeMessage(c *gin.Context) {
	msgID := c.Param("id")

	mediaPath, err := postgres.GetMessageMediaPath(msgID)
	if err != nil || mediaPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "arquivo de áudio não encontrado"})
		return
	}

	// mediaPath is like /media/audio/<id>.ogg — map to filesystem
	fsPath := "/app" + mediaPath
	audioData, err := os.ReadFile(fsPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "arquivo de áudio não encontrado no disco"})
		return
	}

	text, err := transcriber.Transcribe(audioData, "audio.ogg")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro na transcrição: " + err.Error()})
		return
	}

	if err := postgres.UpdateMessageContent(msgID, text); err != nil {
		log.Printf("[TRANSCRIBE] Erro ao atualizar mensagem %s: %v", msgID, err)
	}

	c.JSON(http.StatusOK, gin.H{"text": text})
}

func TakeoverConversation(c *gin.Context) {
	name := c.Param("name")
	phone := c.Param("phone")

	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var contactID string
	err := postgres.DB.QueryRow(
		`SELECT id FROM contacts WHERE phone = $1 AND instance_id = $2`,
		phone, inst.ID,
	).Scan(&contactID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "contato não encontrado"})
		return
	}

	postgres.DB.Exec(`UPDATE contacts SET agent_mode = FALSE WHERE id = $1`, contactID)

	payload, _ := json.Marshal(map[string]interface{}{
		"type":       "agent_mode_changed",
		"contact_id": contactID,
		"agent_mode": false,
	})
	hub.Global.Broadcast(payload)

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func ResumeAgent(c *gin.Context) {
	name := c.Param("name")
	phone := c.Param("phone")

	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var contactID string
	err := postgres.DB.QueryRow(
		`SELECT id FROM contacts WHERE phone = $1 AND instance_id = $2`,
		phone, inst.ID,
	).Scan(&contactID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "contato não encontrado"})
		return
	}

	postgres.DB.Exec(`UPDATE contacts SET agent_mode = TRUE WHERE id = $1`, contactID)

	payload, _ := json.Marshal(map[string]interface{}{
		"type":       "agent_mode_changed",
		"contact_id": contactID,
		"agent_mode": true,
	})
	hub.Global.Broadcast(payload)

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// MarkAsRead zera o unread_count do contato quando o atendente abre a conversa.
func MarkAsRead(c *gin.Context) {
	instanceName := c.Param("name")
	phone := c.Param("phone")

	_, err := postgres.DB.Exec(`
		UPDATE contacts SET unread_count = 0
		WHERE instance_id = (SELECT id FROM instances WHERE name = $1)
		AND phone = $2
	`, instanceName, phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// SendFromUI envia mensagem de texto autenticando via JWT (uso interno da UI de conversas).
func SendFromUI(c *gin.Context) {
	instanceName := c.Param("name")
	phone := c.Param("phone")

	inst, ok := instance.Global.GetByName(instanceName)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message é obrigatório"})
		return
	}

	number := strings.TrimPrefix(phone, "+")
	number = strings.ReplaceAll(number, " ", "")

	// Salva no DB imediatamente (sem esperar o WhatsApp)
	var contactID string
	if err := postgres.DB.QueryRow(
		`INSERT INTO contacts (instance_id, phone, name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (instance_id, phone) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
		inst.ID, number, number,
	).Scan(&contactID); err != nil {
		log.Printf("[DB] Erro ao upsert contact (sendUI): %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar contato"})
		return
	}

	msgID := uuid.New().String()
	var createdAt string
	if err := postgres.DB.QueryRow(
		`INSERT INTO messages (id, instance_id, contact_id, direction, content, type)
		 VALUES ($1, $2, $3, 'out', $4, 'text')
		 RETURNING created_at`,
		msgID, inst.ID, contactID, req.Message,
	).Scan(&createdAt); err != nil {
		log.Printf("[DB] Erro ao salvar msg enviada: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar mensagem"})
		return
	}

	// Broadcast via WS antes de retornar
	payload := map[string]interface{}{
		"event": "new_message",
		"data": map[string]interface{}{
			"id":            msgID,
			"instance_id":   inst.ID,
			"instance_name": inst.Name,
			"contact_id":    contactID,
			"phone":         number,
			"name":          number,
			"direction":     "out",
			"content":       req.Message,
			"type":          "text",
			"created_at":    createdAt,
		},
	}
	jsonBytes, _ := json.Marshal(payload)
	hub.Global.Broadcast(jsonBytes)

	// Desativa agente quando humano envia mensagem
	postgres.DB.Exec(`UPDATE contacts SET agent_mode = FALSE WHERE id = $1`, contactID)
	agentPayload, _ := json.Marshal(map[string]interface{}{
		"type":       "agent_mode_changed",
		"contact_id": contactID,
		"agent_mode": false,
	})
	hub.Global.Broadcast(agentPayload)

	// Retorna imediatamente — sem travar o frontend
	c.JSON(http.StatusOK, gin.H{"ok": true})

	// Envia para o WhatsApp em background com indicador de digitação
	go func() {
		if err := service.SendText(inst, number, req.Message); err != nil {
			log.Printf("[SEND] Erro ao enviar para WhatsApp: %v", err)
		}
	}()
}
