package handler

import (
	"html/template"
	"net/http"
	"botwapp/internal/auth"
	"botwapp/internal/instance"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

func LoadTemplates() {} // mantido para compatibilidade com main.go

func render(c *gin.Context, status int, name string, data gin.H) {
	t, err := template.ParseFiles("web/templates/layout.html", "web/templates/"+name)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(c.Writer, "layout.html", data); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
	}
}

func WebAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("wapi_token")
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		claims, err := auth.ValidateToken(token)
		if err != nil {
			c.SetCookie("wapi_token", "", -1, "/", "", false, false)
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		var role, companyID string
		postgres.DB.QueryRow(`SELECT COALESCE(role,'admin'), COALESCE(company_id::text,'') FROM users WHERE id = $1`, claims.UserID).Scan(&role, &companyID)

		c.Set("token", token)
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", role)
		c.Set("company_id", companyID)
		c.Next()
	}
}

func WebLogin(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		render(c, http.StatusOK, "login.html", gin.H{})
		return
	}

	username := c.PostForm("username")
	password := c.PostForm("password")

	token, err := auth.Login(username, password)
	if err != nil {
		render(c, http.StatusUnauthorized, "login.html", gin.H{"Error": "Usuário ou senha inválidos"})
		return
	}

	c.SetCookie("wapi_token", token, 86400, "/", "", false, false)
	c.Redirect(http.StatusSeeOther, "/connections")
}

func WebLogout(c *gin.Context) {
	c.SetCookie("wapi_token", "", -1, "/", "", false, false)
	c.Redirect(http.StatusFound, "/login")
}

func WebConversations(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	render(c, http.StatusOK, "conversations.html", gin.H{
		"Token":    token,
		"Username": username,
	})
}

func WebConnections(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")

	type InstView struct {
		Name   string
		Status string
		Phone  string
	}

	all := instance.Global.GetAll()
	views := make([]InstView, 0, len(all))
	for _, inst := range all {
		status := "disconnected"
		if inst.Status == "connected" && inst.Phone != "" {
			status = "connected"
		}
		views = append(views, InstView{
			Name:   inst.Name,
			Status: status,
			Phone:  inst.Phone,
		})
	}

	render(c, http.StatusOK, "connections.html", gin.H{
		"Token":     token,
		"Username":  username,
		"Instances": views,
	})
}
