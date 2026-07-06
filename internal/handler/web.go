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
	userID := currentUserID(c)
	render(c, http.StatusOK, "conversations.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     currentRole(c),
		"UserID":   userID,
	})
}

func WebApiDocs(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")

	type InstView struct {
		Name   string
		APIKey string
		Status string
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
			APIKey: inst.APIKey,
			Status: status,
		})
	}

	render(c, http.StatusOK, "api-docs.html", gin.H{
		"Token":     token,
		"Username":  username,
		"Role":      currentRole(c),
		"Instances": views,
	})
}

func WebTeam(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	render(c, http.StatusOK, "users.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     currentRole(c),
	})
}

func WebSectors(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	render(c, http.StatusOK, "sectors.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     currentRole(c),
	})
}

func WebProducts(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	render(c, http.StatusOK, "products.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     currentRole(c),
	})
}

func WebConnections(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	companyID := currentCompanyID(c)

	type InstView struct {
		ID                  string
		Name                string
		Status              string
		Phone               string
		FirstContactAgentID string
		ReturningAgentID    string
		SectorIDs           string // JSON array string
	}

	all := instance.Global.GetAll()
	views := make([]InstView, 0, len(all))
	for _, inst := range all {
		status := "disconnected"
		if inst.Status == "connected" && inst.Phone != "" {
			status = "connected"
		}
		var firstAgentID, returningAgentID string
		postgres.DB.QueryRow(
			`SELECT COALESCE(first_contact_agent_id::text,''), COALESCE(returning_agent_id::text,'') FROM instances WHERE id = $1`,
			inst.ID,
		).Scan(&firstAgentID, &returningAgentID)

		// Setor IDs vinculados a esta instância
		sectorRows, _ := postgres.DB.Query(
			`SELECT sector_id::text FROM instance_sectors WHERE instance_id = $1`, inst.ID,
		)
		sectorIDs := []string{}
		if sectorRows != nil {
			for sectorRows.Next() {
				var sid string
				sectorRows.Scan(&sid)
				sectorIDs = append(sectorIDs, `"`+sid+`"`)
			}
			sectorRows.Close()
		}
		sectorJSON := "[" + func() string {
			s := ""
			for i, id := range sectorIDs {
				if i > 0 {
					s += ","
				}
				s += id
			}
			return s
		}() + "]"

		views = append(views, InstView{
			ID:                  inst.ID,
			Name:                inst.Name,
			Status:              status,
			Phone:               inst.Phone,
			FirstContactAgentID: firstAgentID,
			ReturningAgentID:    returningAgentID,
			SectorIDs:           sectorJSON,
		})
	}

	type AgentOption struct {
		ID   string
		Name string
	}
	agentRows, _ := postgres.DB.Query(
		`SELECT id, name FROM agents WHERE company_id = $1 AND is_active = TRUE ORDER BY name ASC`,
		companyID,
	)
	agents := make([]AgentOption, 0)
	if agentRows != nil {
		defer agentRows.Close()
		for agentRows.Next() {
			var a AgentOption
			agentRows.Scan(&a.ID, &a.Name)
			agents = append(agents, a)
		}
	}

	type SectorOption struct {
		ID   string
		Name string
	}
	sectorRows2, _ := postgres.DB.Query(
		`SELECT id, name FROM sectors WHERE company_id = $1 ORDER BY name ASC`, companyID,
	)
	sectors := make([]SectorOption, 0)
	if sectorRows2 != nil {
		defer sectorRows2.Close()
		for sectorRows2.Next() {
			var s SectorOption
			sectorRows2.Scan(&s.ID, &s.Name)
			sectors = append(sectors, s)
		}
	}

	render(c, http.StatusOK, "connections.html", gin.H{
		"Token":     token,
		"Username":  username,
		"Role":      currentRole(c),
		"Instances": views,
		"Agents":    agents,
		"Sectors":   sectors,
	})
}
