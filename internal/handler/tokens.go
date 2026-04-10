package handler

import (
	"net/http"
	"wapi/internal/auth"

	"github.com/gin-gonic/gin"
)

func CreateToken(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name é obrigatório"})
		return
	}
	token, err := auth.CreatePermanentToken(req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, token)
}

func ListTokens(c *gin.Context) {
	tokens, err := auth.ListTokens()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if tokens == nil {
		tokens = []auth.APIToken{}
	}
	c.JSON(http.StatusOK, tokens)
}

func DeleteToken(c *gin.Context) {
	id := c.Param("id")
	if err := auth.DeleteToken(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
