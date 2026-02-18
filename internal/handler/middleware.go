package handler

import (
	"net/http"
	"strings"
	"wapi/internal/auth"
	"wapi/internal/instance"

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

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
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
