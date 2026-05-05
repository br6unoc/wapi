package handler

import (
	"errors"
	"net/http"
	"time"
	"wapi/internal/auth"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func parseUUID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}

type loginRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password" binding:"required"`
}

func Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password é obrigatório"})
		return
	}

	// Aceita tanto "email" quanto "username" como identificador
	identifier := req.Email
	if identifier == "" {
		identifier = req.Username
	}
	if identifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email ou username são obrigatórios"})
		return
	}

	token, err := auth.Login(identifier, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  token,
		"refresh_token": token,
	})
}

func Me(c *gin.Context) {
	userID, _ := c.Get("user_id")
	companyID, _ := c.Get("company_id")
	username, _ := c.Get("username")
	role, _ := c.Get("role")

	// Buscar dados da empresa
	var company model.Company
	companyData := gin.H{}
	if companyID != nil {
		result := postgres.GORM.Where("id = ?", companyID).First(&company)
		if result.Error == nil {
			companyData = gin.H{
				"name":        company.Name,
				"status":           company.Status,
				"expiry_date":      company.ExpiryDate,
				"ai_agent_enabled": company.AiAgentEnabled,
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":         userID,
			"company_id": companyID,
			"username":   username,
			"email":      username,
			"role":       role,
		},
		"company": companyData,
	})
}

func Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "logout realizado"})
}

// --- Gerenciamento de Usuários (ADMIN) ---

func ListUsers(c *gin.Context) {
	companyID, _ := c.Get("company_id")
	var users []model.User
	postgres.GORM.Where("company_id = ?", companyID).Find(&users)

	result := make([]gin.H, 0, len(users))
	for _, u := range users {
		result = append(result, gin.H{
			"id":       u.ID,
			"username": u.Username,
			"email":    u.Email,
			"role":     u.Role,
		})
	}
	c.JSON(http.StatusOK, result)
}

func CreateUser(c *gin.Context) {
	companyID, _ := c.Get("company_id")

	var req struct {
		Username string `json:"username" binding:"required"`
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	if req.Role == "" {
		req.Role = "EDITOR"
	}

	// Verificar se o email já existe
	var existing model.User
	if err := postgres.GORM.Where("email = ?", req.Email).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email já cadastrado"})
		return
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar senha"})
		return
	}

	user := model.User{
		CompanyID:    parseUUID(companyID.(string)),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hashedPassword,
		Role:         req.Role,
	}

	if err := postgres.GORM.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar usuário"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":       user.ID,
		"username": user.Username,
		"email":    user.Email,
		"role":     user.Role,
	})
}

func DeleteUser(c *gin.Context) {
	companyID, _ := c.Get("company_id")
	userID := c.Param("id")

	result := postgres.GORM.Where("id = ? AND company_id = ?", userID, companyID).Delete(&model.User{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "usuário não encontrado"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "usuário removido"})
}

// --- Super Admin: Empresas ---

func ListCompanies(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var companies []model.Company
	postgres.GORM.Find(&companies)

	result := make([]gin.H, 0, len(companies))
	for _, co := range companies {
		// Conta usuários por empresa
		var userCount int64
		postgres.GORM.Model(&model.User{}).Where("company_id = ?", co.ID).Count(&userCount)

		result = append(result, gin.H{
			"id":               co.ID,
			"name":             co.Name,
			"admin_email":      co.AdminEmail,
			"status":           co.Status,
			"user_limit":       co.UserLimit,
			"whatsapp_limit":   co.WhatsappLimit,
			"ai_agent_enabled": co.AiAgentEnabled,
			"expiry_date":      co.ExpiryDate,
			"user_count":       userCount,
		})
	}
	c.JSON(http.StatusOK, result)
}

func CreateCompany(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var req struct {
		Name           string `json:"name" binding:"required"`
		AdminEmail     string `json:"admin_email" binding:"required"`
		AdminPassword  string `json:"password" binding:"required"`
		UserLimit      int    `json:"user_limit"`
		WhatsappLimit  int    `json:"whatsapp_limit"`
		AiAgentEnabled bool   `json:"ai_agent_enabled"`
		ExpiryDate     string `json:"expiry_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos: " + err.Error()})
		return
	}

	// Parse date
	expiry, _ := time.Parse("2006-01-02", req.ExpiryDate)
	if req.ExpiryDate == "" {
		expiry = time.Now().AddDate(0, 1, 0) // Default 30 days
	}

	// Defaults
	if req.UserLimit == 0 {
		req.UserLimit = 5
	}
	if req.WhatsappLimit == 0 {
		req.WhatsappLimit = 1
	}

	// Verifica se o email já existe
	var existing model.User
	if err := postgres.GORM.Where("email = ?", req.AdminEmail).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email já cadastrado"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao verificar email"})
		return
	}

	company := model.Company{
		Name:           req.Name,
		AdminEmail:     req.AdminEmail,
		Status:         "Ativo",
		UserLimit:      req.UserLimit,
		WhatsappLimit:  req.WhatsappLimit,
		AiAgentEnabled: req.AiAgentEnabled,
		ExpiryDate:     expiry,
	}

	if err := postgres.GORM.Create(&company).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar empresa"})
		return
	}

	hashedPassword, err := auth.HashPassword(req.AdminPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar senha"})
		return
	}

	adminUser := model.User{
		CompanyID:    company.ID,
		Username:     "admin",
		Email:        req.AdminEmail,
		PasswordHash: hashedPassword,
		Role:         "ADMIN",
	}
	if err := postgres.GORM.Create(&adminUser).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar admin da empresa"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":     company.ID,
		"name":   company.Name,
		"status": company.Status,
	})
}

func UpdateCompany(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	id := c.Param("id")
	var req struct {
		Name           string `json:"name"`
		AdminEmail     string `json:"admin_email"`
		UserLimit      int    `json:"user_limit"`
		WhatsappLimit  int    `json:"whatsapp_limit"`
		AiAgentEnabled bool   `json:"ai_agent_enabled"`
		ExpiryDate     string `json:"expiry_date"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	var company model.Company
	if err := postgres.GORM.First(&company, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "empresa não encontrada"})
		return
	}

	company.Name = req.Name
	company.AdminEmail = req.AdminEmail
	company.UserLimit = req.UserLimit
	company.WhatsappLimit = req.WhatsappLimit
	company.AiAgentEnabled = req.AiAgentEnabled

	if req.ExpiryDate != "" {
		expiry, _ := time.Parse("2006-01-02", req.ExpiryDate)
		company.ExpiryDate = expiry
	}

	postgres.GORM.Save(&company)
	c.JSON(http.StatusOK, company)
}

func RenewCompany(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	id := c.Param("id")
	var req struct {
		Days int `json:"days"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	var company model.Company
	if err := postgres.GORM.First(&company, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "empresa não encontrada"})
		return
	}

	if company.ExpiryDate.Before(time.Now()) {
		company.ExpiryDate = time.Now().AddDate(0, 0, req.Days)
	} else {
		company.ExpiryDate = company.ExpiryDate.AddDate(0, 0, req.Days)
	}

	postgres.GORM.Save(&company)
	c.JSON(http.StatusOK, gin.H{"message": "renovado com sucesso", "expiry_date": company.ExpiryDate})
}

func UpdateCompanyStatus(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	id := c.Param("id")
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	postgres.GORM.Model(&model.Company{}).Where("id = ?", id).Update("status", req.Status)
	c.JSON(http.StatusOK, gin.H{"message": "status atualizado"})
}

func DeleteCompany(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	id := c.Param("id")
	// Remove usuários da empresa primeiro
	postgres.GORM.Where("company_id = ?", id).Delete(&model.User{})
	// Remove a empresa
	postgres.GORM.Delete(&model.Company{}, "id = ?", id)

	c.JSON(http.StatusOK, gin.H{"message": "empresa removida"})
}

func UpdateAdminPassword(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	id := c.Param("id")
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "senha é obrigatória"})
		return
	}

	hashed, _ := auth.HashPassword(req.Password)
	postgres.GORM.Model(&model.User{}).Where("company_id = ? AND role = ?", id, "ADMIN").Update("password_hash", hashed)

	c.JSON(http.StatusOK, gin.H{"message": "senha atualizada"})
}
