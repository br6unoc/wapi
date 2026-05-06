package main

import (
	"log"
	"os"
	"wapi/config"
	"wapi/internal/auth"
	"wapi/internal/handler"
	"wapi/internal/instance"
	"wapi/internal/service"
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

	// Registrar o hook nativo do SDR: sem webhook, sem configuração manual
	instance.MessageHook = service.ProcessSDRMessage

	// Inicia o motor de disparos em background
	go service.StartDispatcher()

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

	api := r.Group("/api")

	// Auth
	authGroup := api.Group("/auth")
	{
		authGroup.POST("/login", handler.Login)
		authGroup.POST("/logout", handler.Logout)
		authGroup.GET("/me", handler.AuthMiddleware(), handler.Me)
		authGroup.POST("/tokens", handler.AuthMiddleware(), handler.CreateToken)
		authGroup.GET("/tokens", handler.AuthMiddleware(), handler.ListTokens)
		authGroup.DELETE("/tokens/:id", handler.AuthMiddleware(), handler.DeleteToken)
	}

	// Usuários — usa JWT
	usersGroup := api.Group("/users", handler.AuthMiddleware())
	{
		usersGroup.GET("", handler.ListUsers)
		usersGroup.POST("", handler.CreateUser)
		usersGroup.DELETE("/:id", handler.DeleteUser)
	}

	// Empresas (Super Admin) — usa JWT
	companiesGroup := api.Group("/companies", handler.AuthMiddleware())
	{
		companiesGroup.GET("", handler.ListCompanies)
		companiesGroup.POST("", handler.CreateCompany)
		companiesGroup.PUT("/:id", handler.UpdateCompany)
		companiesGroup.PATCH("/:id/renew", handler.RenewCompany)
		companiesGroup.PATCH("/:id/status", handler.UpdateCompanyStatus)
		companiesGroup.DELETE("/:id", handler.DeleteCompany)
		companiesGroup.PATCH("/:id/admin-password", handler.UpdateAdminPassword)
	}

	// Global Config (IA Engine)
	globalGroup := api.Group("/global", handler.AuthMiddleware())
	{
		globalGroup.GET("/config", handler.GetGlobalConfig)
		globalGroup.PUT("/config", handler.UpdateGlobalConfig)
	}

	// Envio — usa API Key
	api.POST("/instances/:name/send/text", handler.APIKeyMiddleware(), handler.SendText)
	api.POST("/instances/:name/send/media", handler.APIKeyMiddleware(), handler.SendMedia)
	api.POST("/instances/:name/send/media-url", handler.APIKeyMiddleware(), handler.SendMediaURL)

	// Config da empresa — usa JWT
	configGroup := api.Group("/config", handler.AuthMiddleware(), handler.SubscriptionMiddleware())
	{
		configGroup.GET("", handler.GetConfig)
		configGroup.PUT("", handler.SaveConfig)
		configGroup.PATCH("", handler.PatchConfig)
		configGroup.PATCH("/motor", handler.PatchConfig)
	}

	// SSE Global da empresa (notificações em tempo real)
	api.GET("/events/stream", handler.AuthMiddleware(), handler.EventsStream)

	// Leads — usa JWT
	api.GET("/leads/qualified_test", handler.ListQualifiedLeads)
	leads := api.Group("/leads", handler.AuthMiddleware(), handler.SubscriptionMiddleware())
	{
		leads.GET("", handler.ListLeads)
		leads.GET("/qualified", handler.ListQualifiedLeads)
		leads.POST("/import", handler.ImportLeads)
		leads.DELETE("/list/:name", handler.DeleteList)
	}

	// SDR — usa JWT
	sdr := api.Group("/sdr", handler.AuthMiddleware(), handler.SubscriptionMiddleware())
	{
		sdr.GET("", handler.GetSDRAgent)
		sdr.PUT("", handler.SaveSDRAgent)
	}

	// Distribuição (Round-Robin) — usa JWT
	distribution := api.Group("/distribution", handler.AuthMiddleware(), handler.SubscriptionMiddleware())
	{
		distribution.GET("", handler.ListDistributors)
		distribution.POST("", handler.CreateDistributor)
		distribution.DELETE("/:id", handler.DeleteDistributor)
	}

	// Instâncias — usa JWT (token via header ou query param para SSE)
	instancesGroup := api.Group("/instances", handler.AuthMiddleware(), handler.SubscriptionMiddleware())
	{
		instancesGroup.GET("", handler.ListInstances)
		instancesGroup.POST("", handler.CreateInstance)
		instancesGroup.GET("/:name", handler.GetInstance)
		instancesGroup.DELETE("/:name", handler.DeleteInstance)
		instancesGroup.GET("/:name/status", handler.GetStatus)
		instancesGroup.GET("/:name/sse", handler.SSEHandler)
		instancesGroup.GET("/:name/qrcode", handler.GetQRCode)
		instancesGroup.GET("/:name/groups", handler.GetGroups)
		instancesGroup.POST("/:name/connect", handler.ConnectInstance)
		instancesGroup.POST("/:name/disconnect", handler.DisconnectInstance)
		instancesGroup.PATCH("/:name/webhook", handler.UpdateWebhook)
		instancesGroup.POST("/:name/apikey", handler.RegenerateAPIKey)
		instancesGroup.PATCH("/:name/config", handler.UpdateConfig)
	}

	// Servir o frontend React
	r.Static("/assets", "./public/assets")
	r.StaticFile("/favicon.ico", "./public/favicon.ico")
	r.StaticFile("/vite.svg", "./public/vite.svg")

	// Rota raiz explícita
	r.GET("/", func(c *gin.Context) {
		c.File("./public/index.html")
	})

	// Fallback para SPA: captura qualquer rota não definida (ex: /login, /dashboard)
	// r.NoRoute NÃO conflita com rotas existentes, diferente do wildcard /*filepath
	r.NoRoute(func(c *gin.Context) {
		if _, err := os.Stat("./public/index.html"); os.IsNotExist(err) {
			log.Println("[ERRO] index.html não encontrado na pasta ./public!")
			c.JSON(404, gin.H{"error": "Frontend não encontrado no container"})
			return
		}
		c.File("./public/index.html")
	})

	// Debug: listar arquivos em ./public ao iniciar
	log.Println("Verificando arquivos em ./public:")
	files, err := os.ReadDir("./public")
	if err != nil {
		log.Printf("Erro ao ler diretório ./public: %v", err)
	} else {
		for _, f := range files {
			log.Printf(" - %s", f.Name())
		}
	}

	log.Printf("Servidor rodando na porta %s", config.App.Port)
	if err := r.Run(":" + config.App.Port); err != nil {
		log.Fatalf("Erro ao iniciar servidor: %v", err)
	}
}

func loadInstancesFromDB() error {
	rows, err := postgres.DB.Query(
		`SELECT id, company_id, name, api_key, webhook_url, transcription_enabled, typing_delay_min, typing_delay_max, status FROM instances`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, companyID, name, apiKey, webhookURL, status string
		var transcriptionEnabled bool
		var typingDelayMin, typingDelayMax int

		if err := rows.Scan(&id, &companyID, &name, &apiKey, &webhookURL, &transcriptionEnabled, &typingDelayMin, &typingDelayMax, &status); err != nil {
			log.Printf("Erro ao ler instância: %v", err)
			continue
		}
		log.Printf("[DB-LOAD] Instância: %s, Status no DB: %s", name, status)

		inst, err := instance.NewInstance(id, companyID, name, apiKey)
		if err != nil {
			log.Printf("Erro ao carregar instância %s: %v", name, err)
			continue
		}

		inst.WebhookURL = webhookURL
		inst.TranscriptionEnabled = transcriptionEnabled
		inst.TypingDelayMin = typingDelayMin
		inst.TypingDelayMax = typingDelayMax
		inst.Status = status

		instance.Global.Add(inst)

		// Auto-conectar se estava conectada
		if inst.Status == "connected" {
			log.Printf("[STARTUP] Auto-conectando instância %s...", name)
			go inst.Connect()
		}

		count++
	}

	log.Printf("%d instância(s) carregada(s) do banco de dados", count)
	return nil
}
