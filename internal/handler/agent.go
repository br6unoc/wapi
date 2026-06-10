package handler

import (
	"net/http"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type Agent struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Prompt          string `json:"prompt"`
	ContactType     string `json:"contact_type"`
	InstanceID      string `json:"instance_id"`
	InstanceName    string `json:"instance_name"`
	IsActive        bool   `json:"is_active"`
	HandoffKeyword  string `json:"handoff_keyword"`
}

type InstanceView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const agentSelectQuery = `
	SELECT a.id, a.name, a.prompt, a.contact_type, a.instance_id, i.name AS instance_name, a.is_active, a.handoff_keyword
	FROM agents a
	JOIN instances i ON i.id = a.instance_id
	WHERE a.company_id = $1
	ORDER BY a.created_at ASC
`

func scanAgent(rows interface {
	Scan(...interface{}) error
}) (Agent, error) {
	var a Agent
	err := rows.Scan(&a.ID, &a.Name, &a.Prompt, &a.ContactType, &a.InstanceID, &a.InstanceName, &a.IsActive, &a.HandoffKeyword)
	return a, err
}

func ListAgents(c *gin.Context) {
	companyID := currentCompanyID(c)

	rows, err := postgres.DB.Query(agentSelectQuery, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	agents := make([]Agent, 0)
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			continue
		}
		agents = append(agents, a)
	}
	c.JSON(http.StatusOK, agents)
}

func CreateAgent(c *gin.Context) {
	var req struct {
		Name           string  `json:"name" binding:"required"`
		Prompt         string  `json:"prompt" binding:"required"`
		ContactType    string  `json:"contact_type" binding:"required"`
		InstanceID     string  `json:"instance_id" binding:"required"`
		IsActive       *bool   `json:"is_active"`
		HandoffKeyword *string `json:"handoff_keyword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campos obrigatórios: name, prompt, contact_type, instance_id"})
		return
	}

	if req.ContactType != "first_contact" && req.ContactType != "returning" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "contact_type deve ser 'first_contact' ou 'returning'"})
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	handoffKeyword := "PRECISO_DE_HUMANO"
	if req.HandoffKeyword != nil && *req.HandoffKeyword != "" {
		handoffKeyword = *req.HandoffKeyword
	}

	companyID := currentCompanyID(c)

	var id string
	err := postgres.DB.QueryRow(`
		INSERT INTO agents (company_id, instance_id, name, prompt, contact_type, is_active, handoff_keyword)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, companyID, req.InstanceID, req.Name, req.Prompt, req.ContactType, isActive, handoffKeyword).Scan(&id)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "já existe um agente deste tipo para esta conexão"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var a Agent
	err = postgres.DB.QueryRow(`
		SELECT a.id, a.name, a.prompt, a.contact_type, a.instance_id, i.name AS instance_name, a.is_active, a.handoff_keyword
		FROM agents a
		JOIN instances i ON i.id = a.instance_id
		WHERE a.id = $1
	`, id).Scan(&a.ID, &a.Name, &a.Prompt, &a.ContactType, &a.InstanceID, &a.InstanceName, &a.IsActive, &a.HandoffKeyword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, a)
}

func UpdateAgent(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)

	// Read existing record
	var existing Agent
	err := postgres.DB.QueryRow(`
		SELECT a.id, a.name, a.prompt, a.contact_type, a.instance_id, i.name AS instance_name, a.is_active, a.handoff_keyword
		FROM agents a
		JOIN instances i ON i.id = a.instance_id
		WHERE a.id = $1 AND a.company_id = $2
	`, id, companyID).Scan(&existing.ID, &existing.Name, &existing.Prompt, &existing.ContactType, &existing.InstanceID, &existing.InstanceName, &existing.IsActive, &existing.HandoffKeyword)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agente não encontrado"})
		return
	}

	var req struct {
		Name           *string `json:"name"`
		Prompt         *string `json:"prompt"`
		ContactType    *string `json:"contact_type"`
		InstanceID     *string `json:"instance_id"`
		IsActive       *bool   `json:"is_active"`
		HandoffKeyword *string `json:"handoff_keyword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	if req.ContactType != nil && *req.ContactType != "first_contact" && *req.ContactType != "returning" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "contact_type deve ser 'first_contact' ou 'returning'"})
		return
	}

	// Apply changes
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Prompt != nil {
		existing.Prompt = *req.Prompt
	}
	if req.ContactType != nil {
		existing.ContactType = *req.ContactType
	}
	if req.InstanceID != nil {
		existing.InstanceID = *req.InstanceID
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if req.HandoffKeyword != nil {
		existing.HandoffKeyword = *req.HandoffKeyword
	}

	res, err := postgres.DB.Exec(`
		UPDATE agents
		SET name = $1, prompt = $2, contact_type = $3, instance_id = $4, is_active = $5, handoff_keyword = $6
		WHERE id = $7 AND company_id = $8
	`, existing.Name, existing.Prompt, existing.ContactType, existing.InstanceID, existing.IsActive, existing.HandoffKeyword, id, companyID)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "já existe um agente deste tipo para esta conexão"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "agente não encontrado"})
		return
	}

	// Re-query to get updated instance_name
	var a Agent
	postgres.DB.QueryRow(`
		SELECT a.id, a.name, a.prompt, a.contact_type, a.instance_id, i.name AS instance_name, a.is_active, a.handoff_keyword
		FROM agents a
		JOIN instances i ON i.id = a.instance_id
		WHERE a.id = $1
	`, id).Scan(&a.ID, &a.Name, &a.Prompt, &a.ContactType, &a.InstanceID, &a.InstanceName, &a.IsActive, &a.HandoffKeyword)

	c.JSON(http.StatusOK, a)
}

func DeleteAgent(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)

	res, err := postgres.DB.Exec(`DELETE FROM agents WHERE id = $1 AND company_id = $2`, id, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "agente não encontrado"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "agente removido"})
}

func WebAgents(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	role := currentRole(c)
	companyID := currentCompanyID(c)

	// Query instances
	instRows, err := postgres.DB.Query(`SELECT id, name FROM instances WHERE company_id = $1 ORDER BY name ASC`, companyID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	defer instRows.Close()

	instances := make([]InstanceView, 0)
	for instRows.Next() {
		var iv InstanceView
		if err := instRows.Scan(&iv.ID, &iv.Name); err != nil {
			continue
		}
		instances = append(instances, iv)
	}

	// Query agents
	agentRows, err := postgres.DB.Query(agentSelectQuery, companyID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	defer agentRows.Close()

	agents := make([]Agent, 0)
	for agentRows.Next() {
		a, err := scanAgent(agentRows)
		if err != nil {
			continue
		}
		agents = append(agents, a)
	}

	render(c, http.StatusOK, "agents.html", gin.H{
		"Token":     token,
		"Username":  username,
		"Role":      role,
		"Instances": instances,
		"Agents":    agents,
	})
}
