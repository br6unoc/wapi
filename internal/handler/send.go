package handler

import (
	"encoding/base64"
	"net/http"
	"strings"
	"wapi/internal/instance"
	"wapi/internal/service"

	"github.com/gin-gonic/gin"
)

func SendText(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		Number  string `json:"number" binding:"required"`
		Message string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "number e message são obrigatórios"})
		return
	}

	number := strings.TrimPrefix(req.Number, "+")
	number = strings.ReplaceAll(number, " ", "")

	if err := service.SendText(inst, number, req.Message); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "mensagem enviada com sucesso"})
}

func SendMedia(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		Number   string `json:"number" binding:"required"`
		Media    string `json:"media" binding:"required"`
		Mimetype string `json:"mimetype" binding:"required"`
		Filename string `json:"filename"`
		Caption  string `json:"caption"`
		Type     string `json:"type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Media)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "media base64 inválido"})
		return
	}

	number := strings.TrimPrefix(req.Number, "+")
	number = strings.ReplaceAll(number, " ", "")

	isAudio := req.Type == "audio" ||
		strings.Contains(req.Mimetype, "audio") ||
		strings.HasSuffix(req.Filename, ".ogg") ||
		strings.HasSuffix(req.Filename, ".mp3") ||
		strings.HasSuffix(req.Filename, ".m4a") ||
		strings.HasSuffix(req.Filename, ".opus")

	if err := service.SendMedia(inst, number, data, req.Mimetype, req.Filename, req.Caption, isAudio); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "mídia enviada com sucesso"})
}
