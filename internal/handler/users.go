package handler

import (
	"encoding/json"
	"net/http"
	"botwapp/internal/auth"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListUsers(c *gin.Context) {
	companyID := currentCompanyID(c)

	rows, err := postgres.DB.Query(`
		SELECT
			u.id, u.username, u.role,
			COALESCE(
				json_agg(DISTINCT jsonb_build_object('id', s.id::text, 'name', s.name))
				FILTER (WHERE s.id IS NOT NULL), '[]'
			)::text AS sectors,
			COALESCE(
				json_agg(DISTINCT jsonb_build_object('id', ins.id::text, 'name', ins.name))
				FILTER (WHERE ins.id IS NOT NULL), '[]'
			)::text AS instances
		FROM users u
		LEFT JOIN user_sectors us ON us.user_id = u.id
		LEFT JOIN sectors s ON s.id = us.sector_id
		LEFT JOIN user_instances ui ON ui.user_id = u.id
		LEFT JOIN instances ins ON ins.id = ui.instance_id
		WHERE u.company_id = $1 AND u.role != 'super_admin'
		GROUP BY u.id, u.username, u.role, u.created_at
		ORDER BY u.created_at ASC
	`, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type UserItem struct {
		ID        string                   `json:"id"`
		Username  string                   `json:"username"`
		Role      string                   `json:"role"`
		Sectors   []map[string]interface{} `json:"sectors"`
		Instances []map[string]interface{} `json:"instances"`
	}

	users := make([]UserItem, 0)
	for rows.Next() {
		var u UserItem
		var sectorsJSON, instancesJSON string
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &sectorsJSON, &instancesJSON); err != nil {
			continue
		}
		json.Unmarshal([]byte(sectorsJSON), &u.Sectors)
		json.Unmarshal([]byte(instancesJSON), &u.Instances)
		if u.Sectors == nil {
			u.Sectors = []map[string]interface{}{}
		}
		if u.Instances == nil {
			u.Instances = []map[string]interface{}{}
		}
		users = append(users, u)
	}
	c.JSON(http.StatusOK, users)
}

func CreateUser(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username e password são obrigatórios"})
		return
	}

	validRoles := map[string]bool{"admin": true, "coordinator": true, "consultant": true}
	if req.Role == "" {
		req.Role = "consultant"
	}
	if !validRoles[req.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role inválida: use admin, coordinator ou consultant"})
		return
	}

	companyID := currentCompanyID(c)

	var usersCount, maxUsers int
	postgres.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE company_id = $1 AND role != 'super_admin'`, companyID).Scan(&usersCount)
	postgres.DB.QueryRow(`SELECT max_users FROM companies WHERE id = $1`, companyID).Scan(&maxUsers)
	if maxUsers > 0 && usersCount >= maxUsers {
		c.JSON(http.StatusForbidden, gin.H{"error": "limite de usuários da empresa atingido"})
		return
	}

	var existing int
	postgres.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username = $1`, req.Username).Scan(&existing)
	if existing > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "username já está em uso"})
		return
	}

	hashed, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar senha"})
		return
	}

	id := uuid.New().String()
	_, err = postgres.DB.Exec(`
		INSERT INTO users (id, username, password, role, company_id)
		VALUES ($1, $2, $3, $4, $5)
	`, id, req.Username, hashed, req.Role, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar usuário"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "username": req.Username, "role": req.Role})
}

func UpdateUser(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)

	var req struct {
		Role        *string `json:"role"`
		NewPassword *string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	if req.Role != nil {
		validRoles := map[string]bool{"admin": true, "coordinator": true, "consultant": true}
		if !validRoles[*req.Role] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role inválida"})
			return
		}
		postgres.DB.Exec(`
			UPDATE users SET role = $1
			WHERE id = $2 AND company_id = $3 AND role != 'super_admin'
		`, *req.Role, id, companyID)
	}

	if req.NewPassword != nil && *req.NewPassword != "" {
		hashed, err := auth.HashPassword(*req.NewPassword)
		if err == nil {
			postgres.DB.Exec(`
				UPDATE users SET password = $1
				WHERE id = $2 AND company_id = $3
			`, hashed, id, companyID)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "usuário atualizado"})
}

func DeleteUser(c *gin.Context) {
	id := c.Param("id")
	companyID := currentCompanyID(c)

	if id == currentUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "não é possível remover a si mesmo"})
		return
	}

	var role string
	postgres.DB.QueryRow(`SELECT role FROM users WHERE id = $1`, id).Scan(&role)
	if role == "super_admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "não é possível remover o super_admin"})
		return
	}

	_, err := postgres.DB.Exec(`DELETE FROM users WHERE id = $1 AND company_id = $2`, id, companyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover usuário"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "usuário removido"})
}

func SetUserAssignments(c *gin.Context) {
	userID := c.Param("id")
	companyID := currentCompanyID(c)

	var count int
	postgres.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE id = $1 AND company_id = $2`, userID, companyID).Scan(&count)
	if count == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "usuário não encontrado"})
		return
	}

	var req struct {
		SectorIDs   []string `json:"sector_ids"`
		InstanceIDs []string `json:"instance_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}
	if req.SectorIDs == nil {
		req.SectorIDs = []string{}
	}
	if req.InstanceIDs == nil {
		req.InstanceIDs = []string{}
	}

	tx, err := postgres.DB.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	tx.Exec(`DELETE FROM user_sectors WHERE user_id = $1`, userID)
	for _, sID := range req.SectorIDs {
		tx.Exec(`INSERT INTO user_sectors (user_id, sector_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, sID)
	}

	tx.Exec(`DELETE FROM user_instances WHERE user_id = $1`, userID)
	for _, iID := range req.InstanceIDs {
		tx.Exec(`INSERT INTO user_instances (user_id, instance_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, iID)
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar atribuições"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "atribuições atualizadas"})
}
