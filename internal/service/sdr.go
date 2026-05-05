package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"wapi/internal/instance"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/google/uuid"
)

type msgBuffer struct {
	timer    *time.Timer
	messages []string
	mu       sync.Mutex
}

var (
	sdrBuffers = make(map[string]*msgBuffer)
	sdrMu      sync.Mutex
)

// ProcessSDRMessage verifica se há um agente SDR ativo e aguarda 20s de silêncio para processar
func ProcessSDRMessage(companyIDStr string, senderNumber string, message string, inst *instance.Instance) {
	companyID, _ := uuid.Parse(companyIDStr)

	// 0. Verifica se a empresa tem o Agente de IA habilitado
	var company model.Company
	if err := postgres.GORM.First(&company, "id = ?", companyID).Error; err != nil {
		log.Printf("[SDR-ERROR] Empresa não encontrada: %v", err)
		return
	}

	if !company.AiAgentEnabled {
		return
	}

	var agent model.SDRAgent
	// Buscar agente SDR ativo para esta empresa
	if err := postgres.GORM.Where("company_id = ? AND is_active = ?", companyID, true).First(&agent).Error; err != nil {
		return
	}

	// Gerenciar buffer de mensagens (20 segundos de espera)
	sdrMu.Lock()
	buf, ok := sdrBuffers[senderNumber]
	if !ok {
		buf = &msgBuffer{}
		sdrBuffers[senderNumber] = buf
	}
	sdrMu.Unlock()

	buf.mu.Lock()
	buf.messages = append(buf.messages, message)
	
	// Resetar ou iniciar o timer
	if buf.timer != nil {
		buf.timer.Stop()
	}
	
	log.Printf("[SDR-BUFFER] Mensagem de %s acumulada. Aguardando 20s de silêncio...", senderNumber)
	
	buf.timer = time.AfterFunc(20*time.Second, func() {
		buf.mu.Lock()
		fullMessage := strings.Join(buf.messages, " ")
		buf.messages = nil // Limpa para a próxima interação
		buf.mu.Unlock()

		// Remove do mapa global
		sdrMu.Lock()
		delete(sdrBuffers, senderNumber)
		sdrMu.Unlock()

		log.Printf("[SDR-BUFFER] 20s atingidos para %s. Processando mensagem completa: %s", senderNumber, fullMessage)
		handleSDRConversation(companyIDStr, senderNumber, fullMessage, inst, agent)
	})
	buf.mu.Unlock()
}

func handleSDRConversation(companyIDStr string, senderNumber string, message string, inst *instance.Instance, agent model.SDRAgent) {
	companyID, _ := uuid.Parse(companyIDStr)

	// Processar em background
	go func() {
		log.Printf("[SDR-MEMORIA] Processando conversa de %s para empresa %s", senderNumber, companyIDStr)

		// 1. Salvar mensagem do usuário no histórico
		userMsg := model.ChatHistory{
			CompanyID:    companyID,
			SenderNumber: senderNumber,
			Role:         "user",
			Content:      message,
		}
		postgres.GORM.Create(&userMsg)

		// 2. Limpeza automática: Manter apenas as últimas 10 mensagens
		var count int64
		postgres.GORM.Model(&model.ChatHistory{}).Where("company_id = ? AND sender_number = ?", companyID, senderNumber).Count(&count)
		if count > 10 {
			postgres.GORM.Where("id IN (?)", 
				postgres.GORM.Model(&model.ChatHistory{}).
					Select("id").
					Where("company_id = ? AND sender_number = ?", companyID, senderNumber).
					Order("created_at asc").
					Limit(int(count - 10)),
			).Delete(&model.ChatHistory{})
		}

		// 3. Buscar histórico para contexto
		var history []model.ChatHistory
		postgres.GORM.Where("company_id = ? AND sender_number = ?", companyID, senderNumber).
			Order("created_at asc").
			Limit(10).
			Find(&history)

		var aiMessages []AIMessage
		for _, h := range history {
			aiMessages = append(aiMessages, AIMessage{
				Role:    h.Role,
				Content: h.Content,
			})
		}

		// 4. Buscar lead atual para injetar nome
		var currentLead model.Lead
		postgres.GORM.Where("company_id = ? AND phone = ?", companyID, senderNumber).First(&currentLead)

		// 5. Instrução oculta de qualificação e contexto adicional
		qualifyInstruction := "\n\nIMPORTANTE: Se você considerar que o lead está qualificado (ex: demonstrou interesse real, informou o que busca, o nome dele e faixa de investimento/localização), adicione ao FINAL da sua resposta exatamente esta tag: [QUALIFIED: {\"name\": \"...\", \"location\": \"...\", \"investment\": \"...\"}]. Preencha os valores coletados no JSON."
		
		if currentLead.Name != "" {
			qualifyInstruction += fmt.Sprintf("\nOBSERVAÇÃO: O nome do cliente é %s. Use este nome na conversa e na tag QUALIFIED.", currentLead.Name)
		}
		
		// 6. Chamar IA com contexto
		// Busca configuração global de IA
		var global model.GlobalConfig
		if err := postgres.GORM.First(&global).Error; err != nil {
			log.Printf("[SDR-ERROR] Configuração global de IA não encontrada: %v", err)
			return
		}

		aiResponse, err := CallAI(
			global.IAProvider,
			global.IAModel,
			global.IAKey,
			agent.PersonaPrompt + qualifyInstruction,
			aiMessages,
			agent.Entropy,
		)

		if err != nil {
			log.Printf("[SDR-ERROR] Falha ao chamar IA: %v", err)
			return
		}

		// 6. Verificar qualificação
		if strings.Contains(aiResponse, "[QUALIFIED:") {
			startIdx := strings.Index(aiResponse, "[QUALIFIED:")
			endIdx := strings.Index(aiResponse[startIdx:], "]")
			
			if endIdx != -1 {
				tagContent := aiResponse[startIdx+11 : startIdx+endIdx]
				var data struct {
					Name       string `json:"name"`
					Location   string `json:"location"`
					Investment string `json:"investment"`
				}
				
				if err := json.Unmarshal([]byte(tagContent), &data); err == nil {
					log.Printf("[SDR-QUALIFICADO] Lead %s qualificado! Nome: %s, Local: %s, Invest: %s", senderNumber, data.Name, data.Location, data.Investment)
					
					now := time.Now()
					// Tenta atualizar o lead existente
					updates := map[string]interface{}{
						"is_qualified":     true,
						"location":         data.Location,
						"investment_value": data.Investment,
						"qualified_at":     &now,
					}
					
					// Só atualiza o nome se o lead original não tinha nome
					if currentLead.Name == "" && data.Name != "" {
						updates["name"] = data.Name
						currentLead.Name = data.Name // Atualiza na memória local
					}

					result := postgres.GORM.Model(&model.Lead{}).
						Where("company_id = ? AND phone = ?", companyID, senderNumber).
						Updates(updates)
					
					// Se o lead não existia (contato direto), cria ele
					if result.RowsAffected == 0 {
						newLead := model.Lead{
							CompanyID:       companyID,
							Name:            data.Name,
							Phone:           senderNumber,
							IsQualified:     true,
							Location:        data.Location,
							InvestmentValue: data.Investment,
							QualifiedAt:     &now,
							Status:          "SENT",
							ListName:        "SDR-AUTO",
						}
						postgres.GORM.Create(&newLead)
					}

					// LOGICA DE DISTRIBUICAO (ROUND-ROBIN)
					var distributor model.Distributor
					errDist := postgres.GORM.Where("company_id = ? AND is_active = ?", companyID, true).
						Order("last_assigned_at asc nulls first, leads_count asc").
						First(&distributor).Error

					if errDist == nil {
						distributor.LeadsCount += 1
						distributor.LastAssignedAt = &now
						postgres.GORM.Save(&distributor)

						// Pega o nome mais atualizado (se currentLead tiver vazio e o data.Name preencheu, ou vice-versa)
						finalName := currentLead.Name
						if finalName == "" {
							finalName = data.Name
						}

						msg := fmt.Sprintf("🔔 *Novo Lead Qualificado!*\n\n*Nome:* %s\n*Telefone:* %s\n*Local:* %s\n*Investimento:* %s", 
							finalName, senderNumber, data.Location, data.Investment)

						errSend := SendText(inst, distributor.Phone, msg)
						if errSend != nil {
							log.Printf("[SDR-DISTRIBUICAO] Erro ao enviar notificacao para %s: %v", distributor.Phone, errSend)
						} else {
							log.Printf("[SDR-DISTRIBUICAO] Notificacao enviada para o atendente %s (%s)", distributor.Name, distributor.Phone)
						}
					} else {
						log.Printf("[SDR-DISTRIBUICAO] Nenhum distribuidor disponivel para a empresa %s", companyIDStr)
					}

					// Notifica o frontend via SSE para atualizar a lista em tempo real
					inst.BroadcastSSE(`{"event":"lead_update","data":{}}`)
				}
				
				aiResponse = strings.TrimSpace(aiResponse[:startIdx] + aiResponse[startIdx+endIdx+1:])
			}
		}

		// 7. Quebrar resposta em blocos
		parts := splitMessage(aiResponse, 3)

		// 8. Enviar com simulação humana
		for _, part := range parts {
			if part == "" { continue }

			assistantMsg := model.ChatHistory{
				CompanyID:    companyID,
				SenderNumber: senderNumber,
				Role:         "assistant",
				Content:      part,
			}
			postgres.GORM.Create(&assistantMsg)

			err = SendText(inst, senderNumber, part)
			if err != nil {
				log.Printf("[SDR-ERROR] Falha ao enviar: %v", err)
			}

			time.Sleep(time.Duration(2+ (len(part)/50)) * time.Second)
		}

		log.Printf("[SDR-MEMORIA] Fluxo concluído para %s", senderNumber)
	}()
}

// splitMessage quebra o texto em blocos humanos de no máximo 3 linhas ou por pontuação (. ? !)
func splitMessage(text string, maxLines int) []string {
	// 1. Quebra por parágrafos duplos primeiro (divisão natural)
	paragraphs := strings.Split(text, "\n\n")
	var finalParts []string

	for _, p := range paragraphs {
		// 2. Tenta quebrar por pontuação (. ? !)
		// Primeiro normalizamos as pontuações para facilitar o split
		tempP := strings.ReplaceAll(p, "? ", "?|")
		tempP = strings.ReplaceAll(tempP, "! ", "!|")
		tempP = strings.ReplaceAll(tempP, ". ", ".|")
		
		sentences := strings.Split(tempP, "|")
		var currentBlock []string
		
		for _, s := range sentences {
			trimmed := strings.TrimSpace(s)
			if trimmed == "" { continue }
			
			currentBlock = append(currentBlock, trimmed)
			
			// Se o bloco já tem 2 sentenças ou atingiu o limite de linhas, fecha o balão
			if len(currentBlock) >= 2 || strings.Count(strings.Join(currentBlock, " "), "\n") >= maxLines-1 {
				finalParts = append(finalParts, strings.Join(currentBlock, " "))
				currentBlock = []string{}
			}
		}
		
		if len(currentBlock) > 0 {
			finalParts = append(finalParts, strings.Join(currentBlock, " "))
		}
	}

	return finalParts
}
