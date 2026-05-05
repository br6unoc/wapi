package handler

import (
	"net/http"
	"wapi/internal/instance"
	"wapi/internal/model"
	"wapi/internal/service"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Helper para pegar companyID do contexto (injetado pelo AuthMiddleware)
func getCompanyID(c *gin.Context) (string, bool) {
	val, exists := c.Get("company_id")
	if !exists {
		return "", false
	}
	return val.(string), true
}

func ListInstances(c *gin.Context) {
	companyID, ok := getCompanyID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autorizado"})
		return
	}

	instances := instance.Global.GetAll()
	result := make([]gin.H, 0)
	for _, inst := range instances {
		if inst.CompanyID != companyID {
			continue // Pula instâncias de outras empresas
		}

		actualStatus := "disconnected"
		actualPhone := inst.Phone

		if inst.Status == "connected" && inst.Phone != "" {
			actualStatus = "connected"
		}

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
	companyID, ok := getCompanyID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autorizado"})
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome é obrigatório"})
		return
	}

	// Verifica limite de instâncias
	var company model.Company
	if err := postgres.GORM.First(&company, "id = ?", companyID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao verificar limites"})
		return
	}

	var instanceCount int64
	postgres.GORM.Model(&model.Instance{}).Where("company_id = ?", companyID).Count(&instanceCount)

	if int(instanceCount) >= company.WhatsappLimit {
		c.JSON(http.StatusForbidden, gin.H{
			"error": "Limite de instâncias atingido para o seu plano. Entre em contato com o suporte para aumentar seu limite.",
		})
		return
	}

	id := uuid.New().String()
	apiKey := uuid.New().String()

	inst, err := instance.NewInstance(id, companyID, req.Name, apiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_, err = postgres.DB.Exec(
		`INSERT INTO instances (id, company_id, name, api_key) VALUES ($1, $2, $3, $4)`,
		id, companyID, req.Name, apiKey,
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

// Para as demais rotas que pegam pelo 'name', precisamos garantir que a instância pertence ao companyID
func getInstanceOwned(c *gin.Context) (*instance.Instance, bool) {
	companyID, ok := getCompanyID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autorizado"})
		return nil, false
	}
	name := c.Param("name")
	inst, found := instance.Global.GetByName(name)
	if !found || inst.CompanyID != companyID {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return nil, false
	}
	return inst, true
}

func GetInstance(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":                    inst.ID,
		"name":                  inst.Name,
		"status":                inst.Status,
		"phone":                 inst.Phone,
		"webhook_url":           inst.WebhookURL,
		"transcription_enabled": inst.TranscriptionEnabled,
		"typing_delay_min":      inst.TypingDelayMin,
		"typing_delay_max":      inst.TypingDelayMax,
		"api_key":               inst.APIKey,
	})
}

func DeleteInstance(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
		return
	}

	instance.Global.Remove(inst.ID)
	postgres.DB.Exec(`DELETE FROM instances WHERE id = $1`, inst.ID)

	c.JSON(http.StatusOK, gin.H{"message": "instância removida com sucesso"})
}

func GetStatus(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
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
	inst, ok := getInstanceOwned(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"qrcode": inst.LastQR, "status": inst.Status})
}

func ConnectInstance(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
		return
	}

	if err := inst.Connect(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "conectando..."})
}

func DisconnectInstance(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
		return
	}

	inst.Logout()
	postgres.DB.Exec(`UPDATE instances SET status = 'disconnected' WHERE id = $1`, inst.ID)

	c.JSON(http.StatusOK, gin.H{"message": "instância desconectada"})
}

func UpdateWebhook(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
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
	inst, ok := getInstanceOwned(c)
	if !ok {
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
	inst, ok := getInstanceOwned(c)
	if !ok {
		return
	}

	newKey := uuid.New().String()
	inst.APIKey = newKey
	postgres.DB.Exec(`UPDATE instances SET api_key = $1 WHERE id = $2`, newKey, inst.ID)

	c.JSON(http.StatusOK, gin.H{"api_key": newKey})
}

// ATENÇÃO: Essa rota pode ser chamada sem JWT se o front não passar, 
// mas no main.go agrupamos com AuthMiddleware, então o token estará presente.
func SSEHandler(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
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
		c.SSEvent("message", `{"event":"qr","data":{"qrcode":"`+inst.LastQR+`"}}`)
		c.Writer.Flush()
	}
	
	if inst.Status == "connected" && inst.Phone != "" {
		c.SSEvent("message", `{"event":"connected","data":{"phone":"`+inst.Phone+`","qrcode":""}}`)
		c.Writer.Flush()
	}

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-inst.Ctx():
			return
		case msg := <-ch:
			c.SSEvent("message", msg)
			c.Writer.Flush()
		}
	}
}

func GetGroups(c *gin.Context) {
	inst, ok := getInstanceOwned(c)
	if !ok {
		return
	}

	groups, err := service.GetGroups(inst)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, groups)
}
