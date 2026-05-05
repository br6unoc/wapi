package handler

import (
	"net/http"
	"time"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func SubscriptionMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		companyIDStr, exists := c.Get("company_id")
		if !exists {
			c.Next()
			return
		}

		// SUPER_ADMIN ignora trava de assinatura
		role, _ := c.Get("role")
		if role == "SUPER_ADMIN" {
			c.Next()
			return
		}

		companyID, err := uuid.Parse(companyIDStr.(string))
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "ID de empresa inválido"})
			c.Abort()
			return
		}

		var company model.Company
		if err := postgres.GORM.First(&company, "id = ?", companyID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Empresa não encontrada"})
			c.Abort()
			return
		}

		// Verifica se está expirado
		if time.Now().After(company.ExpiryDate) {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error": "Assinatura expirada. Por favor, realize a renovação.",
				"expired": true,
			})
			c.Abort()
			return
		}

		// Verifica status manual
		if company.Status != "Ativo" {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Acesso bloqueado por inadimplência ou decisão administrativa.",
				"blocked": true,
			})
			c.Abort()
			return
		}

		// Adiciona dados da empresa ao contexto para uso posterior
		c.Set("company_obj", company)
		c.Next()
	}
}
