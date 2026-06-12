package handler

import (
	"net/http"
	"strings"
	"botwapp/internal/auth"
	"botwapp/internal/instance"
	"botwapp/store/postgres"

	"github.com/gin-gonic/gin"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token não informado"})
			c.Abort()
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "formato inválido, use: Bearer <token>"})
			c.Abort()
			return
		}

		claims, err := auth.ValidateToken(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token inválido"})
			c.Abort()
			return
		}

		var role, companyID string
		postgres.DB.QueryRow(`SELECT COALESCE(role,'admin'), COALESCE(company_id::text,'') FROM users WHERE id = $1`, claims.UserID).Scan(&role, &companyID)

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", role)
		c.Set("company_id", companyID)
		c.Next()
	}
}

func AdminOrAbove() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role != "admin" && role != "super_admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func SuperAdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if role != "super_admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func currentCompanyID(c *gin.Context) string {
	v, _ := c.Get("company_id")
	s, _ := v.(string)
	return s
}

func currentRole(c *gin.Context) string {
	v, _ := c.Get("role")
	s, _ := v.(string)
	return s
}

func currentUserID(c *gin.Context) string {
	v, _ := c.Get("user_id")
	s, _ := v.(string)
	return s
}

func APIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("apikey")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key não informada"})
			c.Abort()
			return
		}

		instanceName := c.Param("name")
		inst, ok := instance.Global.GetByName(instanceName)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
			c.Abort()
			return
		}

		if inst.APIKey != apiKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key inválida"})
			c.Abort()
			return
		}

		c.Set("instance", inst)
		c.Next()
	}
}
