package handler

import (
	"net/http"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

type Agent struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Prompt            string `json:"prompt"`
	IsActive          bool   `json:"is_active"`
	HandoffKeyword    string `json:"handoff_keyword"`
	FollowupEnabled   bool   `json:"followup_enabled"`
	FollowupIntervals string `json:"followup_intervals"` // JSON array string, e.g. [120,1440,4320]
	FollowupMax       int    `json:"followup_max"`
}

const agentSelectQuery = `
	SELECT id, name, prompt, is_active, handoff_keyword,
	       followup_enabled, followup_intervals::text, followup_max
	FROM agents
	WHERE company_id = $1
	ORDER BY created_at ASC
`

func scanAgent(rows interface {
	Scan(...interface{}) error
}) (Agent, error) {
	var a Agent
	err := rows.Scan(&a.ID, &a.Name, &a.Prompt, &a.IsActive, &a.HandoffKeyword,
		&a.FollowupEnabled, &a.FollowupIntervals, &a.FollowupMax)
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
		Name              string  `json:"name" binding:"required"`
		Prompt            string  `json:"prompt" binding:"required"`
		IsActive          *bool   `json:"is_active"`
		HandoffKeyword    *string `json:"handoff_keyword"`
		FollowupEnabled   *bool   `json:"followup_enabled"`
		FollowupIntervals *string `json:"followup_intervals"`
		FollowupMax       *int    `json:"followup_max"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campos obrigatórios: name, prompt"})
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
	followupEnabled := false
	if req.FollowupEnabled != nil {
		followupEnabled = *req.FollowupEnabled
	}
	followupIntervals := "[120, 1440, 4320]"
	if req.FollowupIntervals != nil && *req.FollowupIntervals != "" {
		followupIntervals = *req.FollowupIntervals
	}
	followupMax := 3
	if req.FollowupMax != nil {
		followupMax = *req.FollowupMax
	}

	companyID := currentCompanyID(c)
	var id string
	err := postgres.DB.QueryRow(`
		INSERT INTO agents (company_id, name, prompt, is_active, handoff_keyword,
		                    followup_enabled, followup_intervals, followup_max)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8)
		RETURNING id
	`, companyID, req.Name, req.Prompt, isActive, handoffKeyword,
		followupEnabled, followupIntervals, followupMax).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var a Agent
	postgres.DB.QueryRow(
		`SELECT id, name, prompt, is_active, handoff_keyword,
		        followup_enabled, followup_intervals::text, followup_max
		 FROM agents WHERE id = $1`, id,
	).Scan(&a.ID, &a.Name, &a.Prompt, &a.IsActive, &a.HandoffKeyword,
		&a.FollowupEnabled, &a.FollowupIntervals, &a.FollowupMax)

	c.JSON(http.StatusCreated, a)
}

func UpdateAgent(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)

	var existing Agent
	err := postgres.DB.QueryRow(`
		SELECT id, name, prompt, is_active, handoff_keyword,
		       followup_enabled, followup_intervals::text, followup_max
		FROM agents WHERE id = $1 AND company_id = $2
	`, id, companyID).Scan(&existing.ID, &existing.Name, &existing.Prompt,
		&existing.IsActive, &existing.HandoffKeyword,
		&existing.FollowupEnabled, &existing.FollowupIntervals, &existing.FollowupMax)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "agente não encontrado"})
		return
	}

	var req struct {
		Name              *string `json:"name"`
		Prompt            *string `json:"prompt"`
		IsActive          *bool   `json:"is_active"`
		HandoffKeyword    *string `json:"handoff_keyword"`
		FollowupEnabled   *bool   `json:"followup_enabled"`
		FollowupIntervals *string `json:"followup_intervals"`
		FollowupMax       *int    `json:"followup_max"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Prompt != nil {
		existing.Prompt = *req.Prompt
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if req.HandoffKeyword != nil {
		existing.HandoffKeyword = *req.HandoffKeyword
	}
	if req.FollowupEnabled != nil {
		existing.FollowupEnabled = *req.FollowupEnabled
	}
	if req.FollowupIntervals != nil && *req.FollowupIntervals != "" {
		existing.FollowupIntervals = *req.FollowupIntervals
	}
	if req.FollowupMax != nil {
		existing.FollowupMax = *req.FollowupMax
	}

	res, err := postgres.DB.Exec(`
		UPDATE agents SET name=$1, prompt=$2, is_active=$3, handoff_keyword=$4,
		                  followup_enabled=$5, followup_intervals=$6::jsonb, followup_max=$7
		WHERE id=$8 AND company_id=$9
	`, existing.Name, existing.Prompt, existing.IsActive, existing.HandoffKeyword,
		existing.FollowupEnabled, existing.FollowupIntervals, existing.FollowupMax,
		id, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "agente não encontrado"})
		return
	}

	c.JSON(http.StatusOK, existing)
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
		"Token":    token,
		"Username": username,
		"Role":     role,
		"Agents":   agents,
	})
}
