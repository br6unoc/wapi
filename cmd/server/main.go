package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"botwapp/config"
	"botwapp/internal/auth"
	"botwapp/internal/handler"
	"botwapp/internal/instance"
	"botwapp/store/postgres"
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
	r.MaxMultipartMemory = 32 << 20 // 32 MB

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
		authGroup.POST("/tokens", handler.AuthMiddleware(), handler.CreateToken)
		authGroup.GET("/tokens", handler.AuthMiddleware(), handler.ListTokens)
		authGroup.DELETE("/tokens/:id", handler.AuthMiddleware(), handler.DeleteToken)
	}

	// SSE, QR Code e WebSocket — sem autenticação de header
	r.GET("/instances/:name/sse", handler.SSEHandler)
	r.GET("/instances/:name/qrcode", handler.GetQRCode)
	r.GET("/instances/:name/groups", handler.GetGroups)
	r.GET("/ws", handler.WSHandler)

	// Envio — usa API Key
	r.POST("/instances/:name/send/text", handler.APIKeyMiddleware(), handler.SendText)
	r.POST("/instances/:name/send/media", handler.APIKeyMiddleware(), handler.SendMedia)
	r.POST("/instances/:name/send/media-url", handler.APIKeyMiddleware(), handler.SendMediaURL)

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
		instances.PATCH("/:name/agents", handler.UpdateInstanceAgents)
		instances.PATCH("/:name/sectors", handler.UpdateInstanceSectors)
	}

	// API de conversas — JWT
	apiGroup := r.Group("/api", handler.AuthMiddleware())
	{
		apiGroup.GET("/conversations", handler.ListConversations)
		apiGroup.GET("/conversations/unread-count", handler.GetUnreadCount)
		apiGroup.GET("/conversations/:name/:phone/messages", handler.GetMessages)
		apiGroup.POST("/conversations/:name/:phone/send", handler.SendFromUI)
		apiGroup.POST("/conversations/:name/:phone/send-media", handler.SendMediaFromUI)
		apiGroup.POST("/conversations/:name/:phone/read", handler.MarkAsRead)
		apiGroup.POST("/messages/:id/transcribe", handler.TranscribeMessage)
		apiGroup.POST("/conversations/:name/:phone/takeover", handler.TakeoverConversation)
		apiGroup.POST("/conversations/:name/:phone/resume", handler.ResumeAgent)
		apiGroup.PATCH("/conversations/:name/:phone/status", handler.UpdateConvStatus)
		apiGroup.PATCH("/conversations/:name/:phone/assign", handler.AssignConversation)
	}

	// Agentes (admin only)
	agentsAPI := r.Group("/api/agents", handler.AuthMiddleware(), handler.AdminOrAbove())
	agentsAPI.GET("", handler.ListAgents)
	agentsAPI.POST("", handler.CreateAgent)
	agentsAPI.PATCH("/:id", handler.UpdateAgent)
	agentsAPI.DELETE("/:id", handler.DeleteAgent)

	// Produtos
	productsAPI := r.Group("/api/products", handler.AuthMiddleware())
	productsAPI.GET("", handler.APIListProducts)
	productsAPI.POST("", handler.APICreateProduct)
	productsAPI.PUT("/:id", handler.APIUpdateProduct)
	productsAPI.DELETE("/:id", handler.APIDeleteProduct)

	// Empresas (super_admin only)
	companiesAPI := r.Group("/api/companies", handler.AuthMiddleware(), handler.SuperAdminOnly())
	companiesAPI.GET("", handler.ListCompanies)
	companiesAPI.POST("", handler.CreateCompany)
	companiesAPI.GET("/:id", handler.GetCompany)
	companiesAPI.PATCH("/:id", handler.UpdateCompany)
	companiesAPI.DELETE("/:id", handler.DeleteCompany)

	// Usuários e setores (admin only)
	usersAPI := r.Group("/api/users", handler.AuthMiddleware(), handler.AdminOrAbove())
	usersAPI.GET("", handler.ListUsers)
	usersAPI.POST("", handler.CreateUser)
	usersAPI.PATCH("/:id", handler.UpdateUser)
	usersAPI.DELETE("/:id", handler.DeleteUser)
	usersAPI.PUT("/:id/assignments", handler.SetUserAssignments)

	sectorsAPI := r.Group("/api/sectors", handler.AuthMiddleware(), handler.AdminOrAbove())
	sectorsAPI.GET("", handler.ListSectors)
	sectorsAPI.POST("", handler.CreateSector)
	sectorsAPI.PATCH("/:id", handler.UpdateSector)
	sectorsAPI.DELETE("/:id", handler.DeleteSector)

	contactsAPI := r.Group("/api/contacts", handler.AuthMiddleware())
	contactsAPI.POST("/:id/purchase/increment", handler.APIContactPurchaseIncrement)
	contactsAPI.POST("/:id/purchase/decrement", handler.APIContactPurchaseDecrement)
	contactsAPI.POST("/:id/block", handler.APIContactBlock)
	contactsAPI.POST("/:id/unblock", handler.APIContactUnblock)
	contactsAPI.DELETE("/:id", handler.APIContactDelete)
	contactsAPI.GET("/tag-links", handler.ListContactTagLinks)
	contactsAPI.POST("/:id/tags/:tagId", handler.AddContactTag)
	contactsAPI.DELETE("/:id/tags/:tagId", handler.RemoveContactTag)

	tagsAPI := r.Group("/api/tags", handler.AuthMiddleware())
	tagsAPI.GET("", handler.ListTags)
	tagsAPI.POST("", handler.CreateTag)
	tagsAPI.PATCH("/:id", handler.UpdateTag)
	tagsAPI.DELETE("/:id", handler.DeleteTag)

	campaignsAPI := r.Group("/api/campaigns", handler.AuthMiddleware())
	campaignsAPI.GET("", handler.APIListCampaigns)
	campaignsAPI.POST("", handler.APICreateCampaign)
	campaignsAPI.DELETE("/:id", handler.APIDeleteCampaign)
	campaignsAPI.POST("/parse-file", handler.APIParseContactsFile)

	r.Static("/media", "/app/media")

	// Web UI
	handler.LoadTemplates()
	r.GET("/login", handler.WebLogin)
	r.POST("/login", handler.WebLogin)
	r.GET("/logout", handler.WebLogout)
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/connections") })

	webGroup := r.Group("/", handler.WebAuthMiddleware())
	{
		webGroup.GET("/connections", handler.WebConnections)
		webGroup.GET("/conversations", handler.WebConversations)
		webGroup.GET("/settings", handler.WebSettings)
		webGroup.POST("/settings", handler.WebSettingsSave)
		webGroup.GET("/agents", handler.WebAgents)
		webGroup.GET("/api-docs", handler.WebApiDocs)
		webGroup.GET("/companies", handler.WebCompanies)
		webGroup.GET("/team", handler.WebTeam)
		webGroup.GET("/sectors", handler.WebSectors)
		webGroup.GET("/campaigns", handler.WebCampaigns)
		webGroup.GET("/contacts", handler.WebContacts)
		webGroup.GET("/tags", handler.WebTags)
		webGroup.GET("/products", handler.WebProducts)
	}

	r.Static("/web", "./web")

	srv := &http.Server{
		Addr:    ":" + config.App.Port,
		Handler: r,
	}

	// Graceful shutdown: captura SIGTERM/SIGINT
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		log.Printf("Servidor rodando na porta %s", config.App.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Erro ao iniciar servidor: %v", err)
		}
	}()

	go instance.RunFollowupWorker()

	<-quit
	log.Println("Sinal recebido, encerrando servidor graciosamente...")

	// Desconecta todas as instâncias WhatsApp antes de sair
	instance.Global.DisconnectAll()
	log.Println("Instâncias WhatsApp desconectadas.")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Erro no shutdown do servidor: %v", err)
	}
	log.Println("Servidor encerrado.")
}

func loadInstancesFromDB() error {
	rows, err := postgres.DB.Query(
		`SELECT id, name, company_id, api_key, webhook_url, transcription_enabled, typing_delay_min, typing_delay_max, status FROM instances`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	count := 0
	var toReconnect []*instance.Instance

	for rows.Next() {
		var id, name, companyID, apiKey, webhookURL, status string
		var transcriptionEnabled bool
		var typingDelayMin, typingDelayMax int

		if err := rows.Scan(&id, &name, &companyID, &apiKey, &webhookURL, &transcriptionEnabled, &typingDelayMin, &typingDelayMax, &status); err != nil {
			log.Printf("Erro ao ler instância: %v", err)
			continue
		}

		inst, err := instance.NewInstance(id, name, apiKey)
		if err != nil {
			log.Printf("Erro ao carregar instância %s: %v", name, err)
			continue
		}

		inst.CompanyID = companyID
		inst.WebhookURL = webhookURL
		inst.TranscriptionEnabled = transcriptionEnabled
		inst.TypingDelayMin = typingDelayMin
		inst.TypingDelayMax = typingDelayMax
		inst.Status = status

		instance.Global.Add(inst)

		if status == "connected" {
			toReconnect = append(toReconnect, inst)
		}
		count++
	}

	log.Printf("%d instância(s) carregada(s) do banco de dados", count)

	// Reconecta em background as instâncias que estavam conectadas.
	// Se não reconectar em 60s, faz logout no celular.
	if len(toReconnect) > 0 {
		go func() {
			for _, inst := range toReconnect {
				log.Printf("[STARTUP] Reconectando instância %s...", inst.Name)
				if err := inst.Connect(); err != nil {
					log.Printf("[STARTUP] Erro ao reconectar %s: %v", inst.Name, err)
					continue
				}
				// Aguarda até 60s para confirmar reconexão
				go func(i *instance.Instance) {
					deadline := time.Now().Add(60 * time.Second)
					for time.Now().Before(deadline) {
						time.Sleep(5 * time.Second)
						if i.Status == "connected" {
							log.Printf("[STARTUP] Instância %s reconectada com sucesso.", i.Name)
							return
						}
					}
					// Sessão permanece válida — apenas marca desconectado, não faz logout
					log.Printf("[STARTUP] Instância %s não reconectou em 60s, marcando desconectado.", i.Name)
					i.Status = "disconnected"
					i.Phone = ""
					i.SaveStatusToDB()
				}(inst)
			}
		}()
	}

	return nil
}
