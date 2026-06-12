package handler

import (
	"net/http"

	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

func WebSettings(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	groqKey, _ := postgres.GetSetting("groq_api_key")
	companyID := currentCompanyID(c)

	convProvider, _ := postgres.GetCompanySetting(companyID, "conversational_ai_provider")
	convModel, _ := postgres.GetCompanySetting(companyID, "conversational_ai_model")
	convApiKey, _ := postgres.GetCompanySetting(companyID, "conversational_ai_api_key")

	analysisProvider, _ := postgres.GetCompanySetting(companyID, "analysis_ai_provider")
	analysisModel, _ := postgres.GetCompanySetting(companyID, "analysis_ai_model")
	analysisApiKey, _ := postgres.GetCompanySetting(companyID, "analysis_ai_api_key")

	// Fallback para chaves legadas
	if convProvider == "" {
		convProvider, _ = postgres.GetCompanySetting(companyID, "ai_provider")
	}
	if convModel == "" {
		convModel, _ = postgres.GetCompanySetting(companyID, "ai_model")
	}
	if convApiKey == "" {
		convApiKey, _ = postgres.GetCompanySetting(companyID, "ai_api_key")
	}

	render(c, http.StatusOK, "settings.html", gin.H{
		"Token":            token,
		"Username":         username,
		"GroqKey":          groqKey,
		"Saved":            c.Query("saved") == "1",
		"Role":             currentRole(c),
		"ConvProvider":     convProvider,
		"ConvModel":        convModel,
		"ConvApiKey":       convApiKey,
		"AnalysisProvider": analysisProvider,
		"AnalysisModel":    analysisModel,
		"AnalysisApiKey":   analysisApiKey,
	})
}

func WebSettingsSave(c *gin.Context) {
	groqKey := c.PostForm("groq_api_key")
	if err := postgres.SetSetting("groq_api_key", groqKey); err != nil {
		c.String(http.StatusInternalServerError, "Erro ao salvar configuração")
		return
	}

	companyID := currentCompanyID(c)

	if v := c.PostForm("conv_provider"); v != "" {
		postgres.SetCompanySetting(companyID, "conversational_ai_provider", v)
	}
	if v := c.PostForm("conv_model"); v != "" {
		postgres.SetCompanySetting(companyID, "conversational_ai_model", v)
	}
	if v := c.PostForm("conv_api_key"); v != "" {
		postgres.SetCompanySetting(companyID, "conversational_ai_api_key", v)
	}

	if v := c.PostForm("analysis_provider"); v != "" {
		postgres.SetCompanySetting(companyID, "analysis_ai_provider", v)
	}
	if v := c.PostForm("analysis_model"); v != "" {
		postgres.SetCompanySetting(companyID, "analysis_ai_model", v)
	}
	if v := c.PostForm("analysis_api_key"); v != "" {
		postgres.SetCompanySetting(companyID, "analysis_ai_api_key", v)
	}

	c.Redirect(http.StatusSeeOther, "/settings?saved=1")
}
