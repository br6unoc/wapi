package main

import (
	"log"
	"wapi/config"
	"wapi/internal/auth"
	"wapi/internal/handler"
	"wapi/internal/instance"
	"wapi/store/postgres"
        _ "github.com/lib/pq"

	"github.com/gin-gonic/gin"
)

func main() {
	config.Load()

	if err := postgres.Connect(); err != nil {
		log.Fatalf("Erro ao conectar no PostgreSQL: %v", err)
	}
	log.Println("PostgreSQL conectado!")

	if err := postgres.Migrate(); err != nil {
		log.Fatalf("Erro ao executar migrations: %v", err)
	}
	log.Println("Migrations executadas!")

	if err := auth.CreateAdminIfNotExists(config.App.AdminUser, config.App.AdminPassword); err != nil {
		log.Fatalf("Erro ao criar admin: %v", err)
	}
	log.Println("Usuário admin pronto!")

	if err := loadInstancesFromDB(); err != nil {
		log.Printf("Aviso ao carregar instâncias: %v", err)
	}

	r := gin.Default()

	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, apikey")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Auth
	authGroup := r.Group("/auth")
	{
		authGroup.POST("/login", handler.Login)
		authGroup.GET("/me", handler.AuthMiddleware(), handler.Me)
	}

	// SSE e QR Code — sem autenticação
	r.GET("/instances/:name/sse", handler.SSEHandler)
	r.GET("/instances/:name/qrcode", handler.GetQRCode)

	// Envio — usa API Key
	r.POST("/instances/:name/send/text", handler.APIKeyMiddleware(), handler.SendText)
	r.POST("/instances/:name/send/media", handler.APIKeyMiddleware(), handler.SendMedia)

	// Instâncias — usa JWT
	instances := r.Group("/instances", handler.AuthMiddleware())
	{
		instances.GET("", handler.ListInstances)
		instances.POST("", handler.CreateInstance)
		instances.GET("/:name", handler.GetInstance)
		instances.DELETE("/:name", handler.DeleteInstance)
		instances.GET("/:name/status", handler.GetStatus)
		instances.POST("/:name/connect", handler.ConnectInstance)
		instances.POST("/:name/disconnect", handler.DisconnectInstance)
		instances.PATCH("/:name/webhook", handler.UpdateWebhook)
		instances.POST("/:name/apikey", handler.RegenerateAPIKey)
		instances.PATCH("/:name/config", handler.UpdateConfig)
	}

	r.StaticFile("/", "./web/index.html")
	r.Static("/web", "./web")

	log.Printf("Servidor rodando na porta %s", config.App.Port)
	if err := r.Run(":" + config.App.Port); err != nil {
		log.Fatalf("Erro ao iniciar servidor: %v", err)
	}
}

func loadInstancesFromDB() error {
	rows, err := postgres.DB.Query(
		`SELECT id, name, api_key, webhook_url, transcription_enabled, typing_delay_min, typing_delay_max FROM instances`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, name, apiKey, webhookURL string
		var transcriptionEnabled bool
		var typingDelayMin, typingDelayMax int

		if err := rows.Scan(&id, &name, &apiKey, &webhookURL, &transcriptionEnabled, &typingDelayMin, &typingDelayMax); err != nil {
			log.Printf("Erro ao ler instância: %v", err)
			continue
		}

		inst, err := instance.NewInstance(id, name, apiKey)
		if err != nil {
			log.Printf("Erro ao carregar instância %s: %v", name, err)
			continue
		}

		inst.WebhookURL = webhookURL
		inst.TranscriptionEnabled = transcriptionEnabled
		inst.TypingDelayMin = typingDelayMin
		inst.TypingDelayMax = typingDelayMax

		instance.Global.Add(inst)
		count++
	}

	log.Printf("%d instância(s) carregada(s) do banco de dados", count)
	return nil
}
