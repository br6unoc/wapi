package handler

import (
	"net/http"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
)

func GetGlobalConfig(c *gin.Context) {
	var config model.GlobalConfig
	// Busca o primeiro registro ou cria um padrão se não existir
	if err := postgres.GORM.First(&config).Error; err != nil {
		config = model.GlobalConfig{
			IAProvider: "openai",
			IAModel:    "gpt-4o-mini",
		}
		postgres.GORM.Create(&config)
	}
	c.JSON(http.StatusOK, config)
}

func UpdateGlobalConfig(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "SUPER_ADMIN" {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var req model.GlobalConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	var config model.GlobalConfig
	if err := postgres.GORM.First(&config).Error; err != nil {
		postgres.GORM.Create(&req)
		c.JSON(http.StatusOK, req)
		return
	}

	config.IAProvider = req.IAProvider
	config.IAModel = req.IAModel
	config.IAKey = req.IAKey

	postgres.GORM.Save(&config)
	c.JSON(http.StatusOK, config)
}
