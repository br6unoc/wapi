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
	aiProvider, _ := postgres.GetCompanySetting(companyID, "ai_provider")
	aiModel, _ := postgres.GetCompanySetting(companyID, "ai_model")
	aiApiKey, _ := postgres.GetCompanySetting(companyID, "ai_api_key")
	render(c, http.StatusOK, "settings.html", gin.H{
		"Token":      token,
		"Username":   username,
		"GroqKey":    groqKey,
		"Saved":      c.Query("saved") == "1",
		"Role":       currentRole(c),
		"AIProvider": aiProvider,
		"AIModel":    aiModel,
		"AIApiKey":   aiApiKey,
	})
}

func WebSettingsSave(c *gin.Context) {
	groqKey := c.PostForm("groq_api_key")
	if err := postgres.SetSetting("groq_api_key", groqKey); err != nil {
		c.String(http.StatusInternalServerError, "Erro ao salvar configuração")
		return
	}

	companyID := currentCompanyID(c)

	if v := c.PostForm("ai_provider"); v != "" {
		postgres.SetCompanySetting(companyID, "ai_provider", v)
	}
	if v := c.PostForm("ai_model"); v != "" {
		postgres.SetCompanySetting(companyID, "ai_model", v)
	}
	if v := c.PostForm("ai_api_key"); v != "" {
		postgres.SetCompanySetting(companyID, "ai_api_key", v)
	}

	c.Redirect(http.StatusSeeOther, "/settings?saved=1")
}
