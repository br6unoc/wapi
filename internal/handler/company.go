package handler

import (
	"log"
	"net/http"
	"botwapp/internal/auth"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListCompanies(c *gin.Context) {
	rows, err := postgres.DB.Query(`
		SELECT
			co.id, co.name, co.max_users, co.max_instances, co.active, co.plan_value,
			(SELECT COUNT(*) FROM users u WHERE u.company_id = co.id AND u.role != 'super_admin') AS users_count,
			(SELECT COUNT(*) FROM instances i WHERE i.company_id = co.id) AS instances_count
		FROM companies co
		ORDER BY co.created_at ASC
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Company struct {
		ID             string  `json:"id"`
		Name           string  `json:"name"`
		MaxUsers       int     `json:"max_users"`
		MaxInstances   int     `json:"max_instances"`
		Active         bool    `json:"active"`
		PlanValue      float64 `json:"plan_value"`
		UsersCount     int     `json:"users_count"`
		InstancesCount int     `json:"instances_count"`
	}

	companies := make([]Company, 0)
	for rows.Next() {
		var co Company
		if err := rows.Scan(&co.ID, &co.Name, &co.MaxUsers, &co.MaxInstances, &co.Active, &co.PlanValue, &co.UsersCount, &co.InstancesCount); err != nil {
			continue
		}
		companies = append(companies, co)
	}
	c.JSON(http.StatusOK, companies)
}

func CreateCompany(c *gin.Context) {
	var req struct {
		Name          string  `json:"name" binding:"required"`
		MaxUsers      int     `json:"max_users"`
		MaxInstances  int     `json:"max_instances"`
		PlanValue     float64 `json:"plan_value"`
		AdminUsername string  `json:"admin_username" binding:"required"`
		AdminPassword string  `json:"admin_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campos obrigatórios: name, admin_username, admin_password"})
		return
	}
	if req.MaxUsers <= 0 {
		req.MaxUsers = 5
	}
	if req.MaxInstances <= 0 {
		req.MaxInstances = 3
	}

	// Verifica se o username já existe
	var existing int
	postgres.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username = $1`, req.AdminUsername).Scan(&existing)
	if existing > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "username já está em uso"})
		return
	}

	companyID := uuid.New().String()
	_, err := postgres.DB.Exec(`
		INSERT INTO companies (id, name, max_users, max_instances, plan_value)
		VALUES ($1, $2, $3, $4, $5)
	`, companyID, req.Name, req.MaxUsers, req.MaxInstances, req.PlanValue)
	if err != nil {
		log.Printf("[COMPANY] Erro ao criar empresa: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar empresa"})
		return
	}

	hashed, err := auth.HashPassword(req.AdminPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar senha"})
		return
	}

	userID := uuid.New().String()
	_, err = postgres.DB.Exec(`
		INSERT INTO users (id, username, password, role, company_id)
		VALUES ($1, $2, $3, 'admin', $4)
	`, userID, req.AdminUsername, hashed, companyID)
	if err != nil {
		postgres.DB.Exec(`DELETE FROM companies WHERE id = $1`, companyID)
		log.Printf("[COMPANY] Erro ao criar admin da empresa: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar usuário admin"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":             companyID,
		"name":           req.Name,
		"max_users":      req.MaxUsers,
		"max_instances":  req.MaxInstances,
		"plan_value":     req.PlanValue,
		"admin_username": req.AdminUsername,
	})
}

func GetCompany(c *gin.Context) {
	id := c.Param("id")

	var name string
	var maxUsers, maxInstances int
	var active bool
	err := postgres.DB.QueryRow(`SELECT name, max_users, max_instances, active FROM companies WHERE id = $1`, id).
		Scan(&name, &maxUsers, &maxInstances, &active)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "empresa não encontrada"})
		return
	}

	rows, err := postgres.DB.Query(`
		SELECT id, username, role FROM users WHERE company_id = $1 AND role != 'super_admin' ORDER BY created_at ASC
	`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	users := make([]User, 0)
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Username, &u.Role)
		users = append(users, u)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":            id,
		"name":          name,
		"max_users":     maxUsers,
		"max_instances": maxInstances,
		"active":        active,
		"users":         users,
	})
}

func UpdateCompany(c *gin.Context) {
	id := c.Param("id")

	var req struct {
		Name         *string  `json:"name"`
		MaxUsers     *int     `json:"max_users"`
		MaxInstances *int     `json:"max_instances"`
		PlanValue    *float64 `json:"plan_value"`
		Active       *bool    `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	if req.Name != nil {
		postgres.DB.Exec(`UPDATE companies SET name = $1, updated_at = NOW() WHERE id = $2`, *req.Name, id)
	}
	if req.MaxUsers != nil {
		postgres.DB.Exec(`UPDATE companies SET max_users = $1, updated_at = NOW() WHERE id = $2`, *req.MaxUsers, id)
	}
	if req.MaxInstances != nil {
		postgres.DB.Exec(`UPDATE companies SET max_instances = $1, updated_at = NOW() WHERE id = $2`, *req.MaxInstances, id)
	}
	if req.PlanValue != nil {
		postgres.DB.Exec(`UPDATE companies SET plan_value = $1, updated_at = NOW() WHERE id = $2`, *req.PlanValue, id)
	}
	if req.Active != nil {
		postgres.DB.Exec(`UPDATE companies SET active = $1, updated_at = NOW() WHERE id = $2`, *req.Active, id)
	}

	c.JSON(http.StatusOK, gin.H{"message": "empresa atualizada"})
}

func DeleteCompany(c *gin.Context) {
	id := c.Param("id")

	// Não permite deletar a empresa do super_admin
	var superCount int
	postgres.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE company_id = $1 AND role = 'super_admin'`, id).Scan(&superCount)
	if superCount > 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "não é possível remover a empresa do super_admin"})
		return
	}

	_, err := postgres.DB.Exec(`DELETE FROM companies WHERE id = $1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover empresa"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "empresa removida"})
}

func WebCompanies(c *gin.Context) {
	token, _ := c.Get("token")
	username, _ := c.Get("username")
	role, _ := c.Get("role")
	if role != "super_admin" {
		c.Redirect(http.StatusFound, "/connections")
		return
	}
	render(c, http.StatusOK, "companies.html", gin.H{
		"Token":    token,
		"Username": username,
		"Role":     role,
	})
}
