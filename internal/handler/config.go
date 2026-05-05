package handler

import (
	"net/http"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func GetConfig(c *gin.Context) {
	companyID, _ := c.Get("company_id")

	var config model.SystemConfig
	result := postgres.GORM.Where("company_id = ?", companyID).First(&config)
	if result.Error != nil {
		// Retorna config padrão se não existir
		c.JSON(http.StatusOK, gin.H{
			"is_active":    false,
			"interval_min": 180,
			"interval_max": 360,
			"window_start": "08:00",
			"window_end":   "18:00",
			"retry_limit":  3,
			"webhook_url":  "",
			"messages":     []string{""},
			"active_days": map[string]bool{
				"monday": true, "tuesday": true, "wednesday": true,
				"thursday": true, "friday": true, "saturday": true, "sunday": true,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           config.ID,
		"is_active":    config.IsActive,
		"interval_min": config.IntervalMin,
		"interval_max": config.IntervalMax,
		"window_start": config.WindowStart,
		"window_end":   config.WindowEnd,
		"retry_limit":  config.RetryLimit,
		"webhook_url":  config.WebhookURL,
		"messages":     config.Messages,
		"active_days":  config.ActiveDays,
	})
}

func SaveConfig(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, _ := uuid.Parse(companyIDStr.(string))

	var req struct {
		IsActive    bool                   `json:"is_active"`
		IntervalMin int                    `json:"interval_min"`
		IntervalMax int                    `json:"interval_max"`
		WindowStart string                 `json:"window_start"`
		WindowEnd   string                 `json:"window_end"`
		RetryLimit  int                    `json:"retry_limit"`
		WebhookURL  string                 `json:"webhook_url"`
		Messages    []string               `json:"messages"`
		ActiveDays  map[string]interface{} `json:"active_days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	// Busca ou cria a config
	var config model.SystemConfig
	postgres.GORM.Where("company_id = ?", companyID).First(&config)

	config.CompanyID = companyID
	config.IsActive = req.IsActive
	config.IntervalMin = req.IntervalMin
	config.IntervalMax = req.IntervalMax
	config.WindowStart = req.WindowStart
	config.WindowEnd = req.WindowEnd
	config.RetryLimit = req.RetryLimit
	config.WebhookURL = req.WebhookURL

	if req.Messages != nil {
		config.Messages = model.JSONStrings(req.Messages)
	}
	if req.ActiveDays != nil {
		config.ActiveDays = model.JSONMap(req.ActiveDays)
	}

	// Upsert
	if config.ID == uuid.Nil {
		postgres.GORM.Create(&config)
	} else {
		postgres.GORM.Save(&config)
	}

	c.JSON(http.StatusOK, gin.H{"message": "configurações salvas"})
}

func PatchConfig(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, _ := uuid.Parse(companyIDStr.(string))

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	var config model.SystemConfig
	postgres.GORM.Where("company_id = ?", companyID).First(&config)

	// Atualiza apenas os campos enviados
	if v, ok := req["is_active"]; ok {
		config.IsActive = v.(bool)
	}
	if v, ok := req["interval_min"]; ok {
		config.IntervalMin = int(v.(float64))
	}
	if v, ok := req["interval_max"]; ok {
		config.IntervalMax = int(v.(float64))
	}
	if v, ok := req["window_start"]; ok {
		config.WindowStart = v.(string)
	}
	if v, ok := req["window_end"]; ok {
		config.WindowEnd = v.(string)
	}

	config.CompanyID = companyID
	if config.ID == uuid.Nil {
		postgres.GORM.Create(&config)
	} else {
		postgres.GORM.Save(&config)
	}

	c.JSON(http.StatusOK, gin.H{"message": "configuração atualizada"})
}

// SSE global para eventos da empresa (config_update, etc.)
func EventsStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ctx := c.Request.Context()
	<-ctx.Done()
}
