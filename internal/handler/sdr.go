package handler

import (
	"net/http"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func GetSDRAgent(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, _ := uuid.Parse(companyIDStr.(string))

	var agent model.SDRAgent
	result := postgres.GORM.Where("company_id = ?", companyID).First(&agent)
	if result.Error != nil {
		c.JSON(http.StatusOK, nil)
		return
	}

	c.JSON(http.StatusOK, agent)
}

func SaveSDRAgent(c *gin.Context) {
	companyIDStr, _ := c.Get("company_id")
	companyID, _ := uuid.Parse(companyIDStr.(string))

	var req struct {
		AgentName     string        `json:"agent_name"`
		CompanyName   string        `json:"company_name"`
		ProviderIA    string        `json:"provider_ia"`
		ModelName     string        `json:"model_name"`
		ApiKeyIA      string        `json:"api_key_ia"`
		PersonaPrompt string        `json:"persona_prompt"`
		Entropy       float64       `json:"entropy"`
		FunnelSteps   []interface{} `json:"funnel_steps"`
		IsActive      bool          `json:"is_active"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	var agent model.SDRAgent
	postgres.GORM.Where("company_id = ?", companyID).First(&agent)

	agent.CompanyID = companyID
	agent.AgentName = req.AgentName
	agent.CompanyName = req.CompanyName
	agent.ProviderIA = req.ProviderIA
	agent.ModelName = req.ModelName
	agent.ApiKeyIA = req.ApiKeyIA
	agent.PersonaPrompt = req.PersonaPrompt
	agent.Entropy = req.Entropy
	agent.IsActive = req.IsActive

	if req.FunnelSteps != nil {
		agent.FunnelSteps = model.JSONSteps(req.FunnelSteps)
	}

	var err error
	if agent.ID == uuid.Nil {
		err = postgres.GORM.Create(&agent).Error
	} else {
		err = postgres.GORM.Save(&agent).Error
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar agente SDR"})
		return
	}

	c.JSON(http.StatusOK, agent)
}
