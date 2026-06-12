package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"botwapp/internal/instance"
	"botwapp/internal/service"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListInstances(c *gin.Context) {
	instances := instance.Global.GetAll()
	result := make([]gin.H, 0, len(instances))
	for _, inst := range instances {
		// Validação rigorosa: só considera conectado se tiver Phone E status connected
		actualStatus := "disconnected"
		actualPhone := inst.Phone
		
		if inst.Status == "connected" && inst.Phone != "" {
			actualStatus = "connected"
		}
		
		log.Printf("[LIST] Instance %s - Status: %s, Phone: %s", inst.Name, actualStatus, actualPhone)
		result = append(result, gin.H{
			"id":     inst.ID,
			"name":   inst.Name,
			"status": actualStatus,
			"phone":  actualPhone,
		})
	}
	c.JSON(http.StatusOK, result)
}

func CreateInstance(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome é obrigatório"})
		return
	}

	companyID := currentCompanyID(c)
	id := uuid.New().String()
	apiKey := uuid.New().String()

	inst, err := instance.NewInstance(id, req.Name, apiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	inst.CompanyID = companyID

	_, err = postgres.DB.Exec(
		`INSERT INTO instances (id, name, company_id, api_key) VALUES ($1, $2, $3, $4)`,
		id, req.Name, companyID, apiKey,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar instância"})
		return
	}

	instance.Global.Add(inst)

	c.JSON(http.StatusCreated, gin.H{
		"id":      inst.ID,
		"name":    inst.Name,
		"api_key": inst.APIKey,
		"status":  inst.Status,
	})
}

func GetInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var firstAgentID, returningAgentID string
	postgres.DB.QueryRow(
		`SELECT COALESCE(first_contact_agent_id::text,''), COALESCE(returning_agent_id::text,'') FROM instances WHERE id = $1`,
		inst.ID,
	).Scan(&firstAgentID, &returningAgentID)

	c.JSON(http.StatusOK, gin.H{
		"id":                      inst.ID,
		"name":                    inst.Name,
		"status":                  inst.Status,
		"phone":                   inst.Phone,
		"webhook_url":             inst.WebhookURL,
		"transcription_enabled":   inst.TranscriptionEnabled,
		"typing_delay_min":        inst.TypingDelayMin,
		"typing_delay_max":        inst.TypingDelayMax,
		"api_key":                 inst.APIKey,
		"first_contact_agent_id":  firstAgentID,
		"returning_agent_id":      returningAgentID,
	})
}

func UpdateInstanceAgents(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		FirstContactAgentID *string `json:"first_contact_agent_id"`
		ReturningAgentID    *string `json:"returning_agent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	var firstAgentID, returningAgentID interface{}
	if req.FirstContactAgentID != nil && *req.FirstContactAgentID != "" {
		firstAgentID = *req.FirstContactAgentID
	}
	if req.ReturningAgentID != nil && *req.ReturningAgentID != "" {
		returningAgentID = *req.ReturningAgentID
	}

	_, err := postgres.DB.Exec(
		`UPDATE instances SET first_contact_agent_id = $1, returning_agent_id = $2 WHERE id = $3`,
		firstAgentID, returningAgentID, inst.ID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func UpdateInstanceSectors(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		SectorIDs []string `json:"sector_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}
	if req.SectorIDs == nil {
		req.SectorIDs = []string{}
	}

	tx, err := postgres.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	tx.Exec(`DELETE FROM instance_sectors WHERE instance_id = $1`, inst.ID)
	for _, sID := range req.SectorIDs {
		tx.Exec(`INSERT INTO instance_sectors (instance_id, sector_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, inst.ID, sID)
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar setores"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func DeleteInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	instance.Global.Remove(inst.ID)
	postgres.DB.Exec(`DELETE FROM instances WHERE id = $1`, inst.ID)

	c.JSON(http.StatusOK, gin.H{"message": "instância removida com sucesso"})
}

func GetStatus(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	connected := inst.WAClient != nil && inst.WAClient.IsConnected()
	status := inst.Status
	if !connected {
		status = "disconnected"
	}

	c.JSON(http.StatusOK, gin.H{
		"status": status,
		"phone":  inst.Phone,
	})
}

func GetQRCode(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"qrcode": inst.LastQR, "status": inst.Status})
}

func ConnectInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	if err := inst.Connect(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "conectando..."})
}

func DisconnectInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	inst.Disconnect()
	postgres.DB.Exec(`UPDATE instances SET status = 'disconnected' WHERE id = $1`, inst.ID)

	c.JSON(http.StatusOK, gin.H{"message": "instância desconectada"})
}

func UpdateWebhook(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		WebhookURL string `json:"webhook_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	inst.WebhookURL = req.WebhookURL
	postgres.DB.Exec(`UPDATE instances SET webhook_url = $1 WHERE id = $2`, req.WebhookURL, inst.ID)

	c.JSON(http.StatusOK, gin.H{"message": "webhook atualizado"})
}

func UpdateConfig(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		TranscriptionEnabled *bool `json:"transcription_enabled"`
		TypingDelayMin       *int  `json:"typing_delay_min"`
		TypingDelayMax       *int  `json:"typing_delay_max"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	if req.TranscriptionEnabled != nil {
		inst.TranscriptionEnabled = *req.TranscriptionEnabled
	}
	if req.TypingDelayMin != nil {
		inst.TypingDelayMin = *req.TypingDelayMin
	}
	if req.TypingDelayMax != nil {
		inst.TypingDelayMax = *req.TypingDelayMax
	}

	postgres.DB.Exec(
		`UPDATE instances SET transcription_enabled = $1, typing_delay_min = $2, typing_delay_max = $3 WHERE id = $4`,
		inst.TranscriptionEnabled, inst.TypingDelayMin, inst.TypingDelayMax, inst.ID,
	)

	c.JSON(http.StatusOK, gin.H{"message": "configurações salvas"})
}

func RegenerateAPIKey(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	newKey := uuid.New().String()
	inst.APIKey = newKey
	postgres.DB.Exec(`UPDATE instances SET api_key = $1 WHERE id = $2`, newKey, inst.ID)

	c.JSON(http.StatusOK, gin.H{"api_key": newKey})
}

func SSEHandler(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 10)
	inst.AddSSEClient(ch)
	defer inst.RemoveSSEClient(ch)

	if inst.LastQR != "" {
		dataURL := instance.QRToDataURL(inst.LastQR)
		p, _ := json.Marshal(map[string]interface{}{"event": "qr", "data": map[string]string{"qrcode": dataURL}})
		c.SSEvent("message", string(p))
		c.Writer.Flush()
	}

	if inst.Status == "connected" && inst.Phone != "" {
		p, _ := json.Marshal(map[string]interface{}{"event": "connected", "data": map[string]string{"phone": inst.Phone, "qrcode": ""}})
		c.SSEvent("message", string(p))
		c.Writer.Flush()
	}

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			c.SSEvent("message", msg)
			c.Writer.Flush()
		}
	}
}

func GetGroups(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	groups, err := service.GetGroups(inst)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, groups)
}
