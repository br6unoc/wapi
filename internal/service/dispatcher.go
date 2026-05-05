package service

import (
	"fmt"
	"log"
	"strings"
	"time"
	"wapi/internal/instance"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/google/uuid"
)

func replaceName(msg, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		// Remove "{{nome}}" and adjacent spaces nicely
		msg = strings.ReplaceAll(msg, " {{nome}}", "")
		msg = strings.ReplaceAll(msg, "{{nome}} ", "")
		msg = strings.ReplaceAll(msg, "{{nome}}", "")
	} else {
		msg = strings.ReplaceAll(msg, "{{nome}}", name)
	}
	return msg
}

func StartDispatcher() {
	log.Println("🚀 Iniciando Motor de Disparos Nativo (Orion Engine)...")
	for {
		processQueue()
		time.Sleep(15 * time.Second) // Intervalo de varredura
	}
}

func processQueue() {
	var configs []model.SystemConfig
	// Busca apenas empresas com disparos ativos
	result := postgres.GORM.Where("is_active = ?", true).Find(&configs)
	if result.Error != nil {
		return
	}

	for _, config := range configs {
		// Processamento concorrente por empresa para não travar o loop central
		go processCompanyLeads(config)
	}
}

func processCompanyLeads(config model.SystemConfig) {
	if !isInWindow(config) {
		return
	}

	// Busca o próximo lead pendente desta empresa
	var lead model.Lead
	result := postgres.GORM.Where("company_id = ? AND status = ?", config.CompanyID, "PENDING").Order("created_at asc").First(&lead)

	if result.Error != nil {
		return // Sem leads para processar agora
	}

	// Lock básico do lead para evitar processamento duplo em múltiplas instâncias do worker
	lead.Status = "PROCESSING"
	postgres.GORM.Save(&lead)

	// Tenta realizar o envio
	err := dispatchLead(lead, config)

	if err != nil {
		lead.Status = "FAILED"
		log.Printf("❌ Falha no disparo para %s: %v", lead.Phone, err)
	} else {
		lead.Status = "SENT"
		log.Printf("✅ Sucesso no disparo para %s", lead.Phone)
	}

	postgres.GORM.Save(&lead)
}

func isInWindow(config model.SystemConfig) bool {
	now := time.Now()

	// 1. Verificar dia da semana via JSONMap de active_days
	if config.ActiveDays != nil {
		dayKeys := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
		dayKey := dayKeys[int(now.Weekday())]
		if val, exists := config.ActiveDays[dayKey]; exists {
			if active, ok := val.(bool); ok && !active {
				return false
			}
		}
	}

	// 2. Verificar horário (HH:MM)
	currentTime := now.Format("15:04")
	if currentTime < config.WindowStart || currentTime > config.WindowEnd {
		return false
	}

	return true
}

func dispatchLead(lead model.Lead, config model.SystemConfig) error {
	// 1. Localizar instância associada
	var instModel model.Instance
	var err error

	if lead.InstanceID != nil {
		err = postgres.GORM.First(&instModel, "id = ?", lead.InstanceID).Error
	} else {
		// Pega a primeira instância conectada da empresa
		err = postgres.GORM.Where("company_id = ? AND status = ?", lead.CompanyID, "connected").First(&instModel).Error
	}

	if err != nil || instModel.ID == uuid.Nil {
		return fmt.Errorf("nenhuma instância ativa encontrada para a empresa %s", lead.CompanyID)
	}

	inst, ok := instance.Global.GetByName(instModel.Name)
	if !ok {
		return fmt.Errorf("instância %s não carregada na memória do servidor", instModel.Name)
	}

	// 2. Usa mensagens diretamente do tipo JSONStrings
	messages := []string(config.Messages)
	if len(messages) == 0 {
		return fmt.Errorf("configuração de mensagens inválida ou vazia")
	}

	// Seleciona a primeira mensagem disponível
	msg := messages[0]
	
	// Substituição de variáveis
	msg = replaceName(msg, lead.Name)
	
	// 3. Envio nativo via Whatsmeow
	cleanNumber := lead.Phone
	return SendText(inst, cleanNumber, msg)
}
