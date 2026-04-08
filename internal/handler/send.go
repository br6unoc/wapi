package handler

import (
	"encoding/base64"
        "log"
	"fmt"
	"io"
	"path/filepath"
	"time"
	"net/http"
	"strings"
	"wapi/internal/instance"
	"wapi/internal/service"

	"github.com/gin-gonic/gin"
)

type SendMediaURLRequest struct {
	Number  string `json:"number" binding:"required"`
	URL     string `json:"url" binding:"required"`
	Caption string `json:"caption"`
}

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
		Media    string `json:"media"`    // Base64 OU URL
		URL      string `json:"url"`      // Alias para Media
		Mimetype string `json:"mimetype"` // Opcional agora
		Filename string `json:"filename"` // Opcional
		Caption  string `json:"caption"`  // Opcional
		Type     string `json:"type"`     // Opcional
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "number e (media ou url) são obrigatórios"})
		return
	}
	
	// Suportar tanto "media" quanto "url"
	mediaInput := req.Media
	if mediaInput == "" {
		mediaInput = req.URL
	}
	
	if mediaInput == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "media ou url é obrigatório"})
		return
	}
	
	var data []byte
	var mimetype string
	var filename string
	var err error
	
	// Detectar se é URL ou Base64
	if strings.HasPrefix(mediaInput, "http://") || strings.HasPrefix(mediaInput, "https://") {
		// É URL - fazer download
		data, mimetype, filename, err = downloadFromURL(mediaInput)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	} else {
		// É Base64 - decodificar
		data, err = base64.StdEncoding.DecodeString(mediaInput)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "media base64 inválido"})
			return
		}
		
		// Para Base64, mimetype é obrigatório
		if req.Mimetype == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mimetype é obrigatório para Base64"})
			return
		}
		mimetype = req.Mimetype
		filename = req.Filename
	}
	
	// Limpar número
	number := strings.TrimPrefix(req.Number, "+")
	number = strings.ReplaceAll(number, " ", "")
	
	// Detectar se é áudio
	isAudio := req.Type == "audio" ||
		strings.Contains(mimetype, "audio") ||
		strings.HasSuffix(filename, ".ogg") ||
		strings.HasSuffix(filename, ".mp3") ||
		strings.HasSuffix(filename, ".m4a") ||
		strings.HasSuffix(filename, ".opus")
	mediaType := classifyMediaType(mimetype, filename)

	// Vídeo grande: responde imediatamente e processa em background
	if strings.HasPrefix(mimetype, "video/") && len(data) > 16*1024*1024 {
		log.Printf("[ASYNC] Large video (%d bytes), responding immediately", len(data))
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "vídeo em processamento, será enviado em breve",
			"media_type": mediaType,
		})
		go func() {
			if err := service.SendMedia(inst, number, data, mimetype, filename, req.Caption, isAudio); err != nil {
				log.Printf("[ASYNC ERROR] SendMedia failed: %v", err)
			} else {
				log.Printf("[ASYNC SUCCESS] Large video sent - size: %d bytes", len(data))
			}
		}()
		return
	}

	// Mídia normal: comportamento síncrono
	if err := service.SendMedia(inst, number, data, mimetype, filename, req.Caption, isAudio); err != nil {
		log.Printf("[ERROR] SendMedia failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[SUCCESS] Media sent - type: %s, size: %d bytes", mimetype, len(data))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "mídia enviada com sucesso",
		"media_type": mediaType,
	})
}

// Helper: Download de mídia da URL
func downloadFromURL(url string) ([]byte, string, string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	resp, err := client.Get(url)
	if err != nil {
		return nil, "", "", fmt.Errorf("erro ao baixar mídia: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("erro HTTP %d ao baixar mídia", resp.StatusCode)
	}
	
	// Verificar tamanho (limite 25MB)
	if resp.ContentLength > 25*1024*1024 {
		return nil, "", "", fmt.Errorf("arquivo excede o limite de 25MB")
	}
	
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("erro ao ler dados: %w", err)
	}
	
	// Pegar mimetype do header
	mimetype := resp.Header.Get("Content-Type")
	if mimetype == "" || mimetype == "application/octet-stream" {
		// Fallback: detectar pelos primeiros bytes
		mimetype = http.DetectContentType(data)
	}
	
	// Extrair filename da URL
	filename := filepath.Base(url)
	
	return data, mimetype, filename, nil
}

// Helper: Classificar tipo de mídia baseado no mimetype
func classifyMediaType(mimetype, filename string) string {
	// Normalizar mimetype
	mimetype = strings.ToLower(mimetype)
	filename = strings.ToLower(filename)
	
	// Imagem
	if strings.HasPrefix(mimetype, "image/") {
		return "image"
	}
	
	// Vídeo
	if strings.HasPrefix(mimetype, "video/") {
		return "video"
	}
	
	// Áudio/Voz
	if strings.HasPrefix(mimetype, "audio/") {
		// Verificar se é OGG (possível PTT)
		if strings.Contains(mimetype, "ogg") || strings.HasSuffix(filename, ".ogg") {
			return "ptt" // Será tratado como voz
		}
		return "audio"
	}
	
	// Qualquer outro tipo = documento
	return "document"
}

// SendMediaURL - Novo endpoint que aceita URL em vez de Base64
func SendMediaURL(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}
	
	var req SendMediaURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "number e url são obrigatórios"})
		return
	}
	
	// Validar URL
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL deve começar com http:// ou https://"})
		return
	}
	
	// Download da mídia
	data, mimetype, filename, err := downloadFromURL(req.URL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	
	// Classificar tipo de mídia
	mediaType := classifyMediaType(mimetype, filename)
	
	// Limpar número
	number := strings.TrimPrefix(req.Number, "+")
	number = strings.ReplaceAll(number, " ", "")
	
	// Enviar mídia usando o service existente
	isAudio := (mediaType == "audio" || mediaType == "ptt")
	if err := service.SendMedia(inst, number, data, mimetype, filename, req.Caption, isAudio); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "mídia enviada com sucesso",
		"media_type": mediaType,
	})
}
