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
	render(c, http.StatusOK, "settings.html", gin.H{
		"Token":    token,
		"Username": username,
		"GroqKey":  groqKey,
		"Saved":    c.Query("saved") == "1",
	})
}

func WebSettingsSave(c *gin.Context) {
	groqKey := c.PostForm("groq_api_key")
	if err := postgres.SetSetting("groq_api_key", groqKey); err != nil {
		c.String(http.StatusInternalServerError, "Erro ao salvar configuração")
		return
	}
	c.Redirect(http.StatusSeeOther, "/settings?saved=1")
}
