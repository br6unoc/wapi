package instance

import (
	"encoding/json"
	"log"
	"time"

	"botwapp/internal/aiagent"
	"botwapp/store/postgres"
)

// RunFollowupWorker verifica a cada 5 minutos se alguma conversa precisa de follow-up.
func RunFollowupWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		runFollowupCheck()
	}
}

func runFollowupCheck() {
	rows, err := postgres.DB.Query(`
		SELECT
			c.id, c.phone, c.followup_count,
			i.id, i.name,
			i.company_id,
			COALESCE(i.first_contact_agent_id::text, i.returning_agent_id::text, ''),
			m.direction,
			EXTRACT(EPOCH FROM NOW() - m.created_at)::bigint AS elapsed_seconds
		FROM contacts c
		JOIN instances i ON i.id = c.instance_id
		LEFT JOIN LATERAL (
			SELECT direction, created_at FROM messages
			WHERE contact_id = c.id
			ORDER BY created_at DESC LIMIT 1
		) m ON true
		WHERE c.agent_mode = TRUE
		  AND c.conv_status = 'open'
		  AND m.direction = 'out'
		  AND m.created_at IS NOT NULL
	`)
	if err != nil {
		log.Printf("[FOLLOWUP] erro ao buscar conversas: %v", err)
		return
	}
	defer rows.Close()

	type candidate struct {
		contactID      string
		phone          string
		followupCount  int
		instanceID     string
		instanceName   string
		companyID      string
		agentID        string
		elapsedSeconds int64
	}
	var candidates []candidate
	for rows.Next() {
		var cand candidate
		if err := rows.Scan(
			&cand.contactID, &cand.phone, &cand.followupCount,
			&cand.instanceID, &cand.instanceName,
			&cand.companyID, &cand.agentID,
			new(string), &cand.elapsedSeconds,
		); err != nil {
			continue
		}
		if cand.agentID != "" {
			candidates = append(candidates, cand)
		}
	}

	for _, cand := range candidates {
		processFollowupCandidate(cand.contactID, cand.phone, cand.followupCount,
			cand.instanceID, cand.instanceName, cand.companyID, cand.agentID, cand.elapsedSeconds)
	}
}

func processFollowupCandidate(contactID, phone string, followupCount int,
	instanceID, instanceName, companyID, agentID string, elapsedSeconds int64) {

	var followupEnabled bool
	var followupIntervalsJSON string
	var followupMax int
	var agentPrompt, handoffKeyword string
	err := postgres.DB.QueryRow(`
		SELECT followup_enabled, followup_intervals::text, followup_max, prompt, handoff_keyword
		FROM agents WHERE id = $1 AND is_active = TRUE
	`, agentID).Scan(&followupEnabled, &followupIntervalsJSON, &followupMax, &agentPrompt, &handoffKeyword)
	if err != nil || !followupEnabled || followupCount >= followupMax {
		return
	}

	var intervals []int
	if err := json.Unmarshal([]byte(followupIntervalsJSON), &intervals); err != nil || followupCount >= len(intervals) {
		return
	}

	thresholdMinutes := intervals[followupCount]
	elapsedMinutes := int(elapsedSeconds / 60)
	if elapsedMinutes < thresholdMinutes {
		return
	}

	// Reserva atômica: atualiza last_followup_at somente se o threshold passou.
	// Evita duplicatas em caso de múltiplas goroutines concorrentes.
	threshold := time.Now().Add(-time.Duration(thresholdMinutes) * time.Minute)
	var claimedID string
	postgres.DB.QueryRow(`
		UPDATE contacts SET last_followup_at = NOW()
		WHERE id = $1 AND (last_followup_at IS NULL OR last_followup_at < $2)
		RETURNING id
	`, contactID, threshold).Scan(&claimedID)
	if claimedID == "" {
		return
	}

	inst, ok := Global.GetByName(instanceName)
	if !ok {
		return
	}

	// Carregar configuração de AI
	provider, _ := postgres.GetCompanySetting(companyID, "conversational_ai_provider")
	model, _ := postgres.GetCompanySetting(companyID, "conversational_ai_model")
	apiKey, _ := postgres.GetCompanySetting(companyID, "conversational_ai_api_key")
	if provider == "" {
		provider, _ = postgres.GetCompanySetting(companyID, "ai_provider")
	}
	if model == "" {
		model, _ = postgres.GetCompanySetting(companyID, "ai_model")
	}
	if apiKey == "" {
		apiKey, _ = postgres.GetCompanySetting(companyID, "ai_api_key")
	}
	if provider == "" || model == "" || apiKey == "" {
		return
	}

	// Histórico da conversa
	histRows, err := postgres.DB.Query(`
		SELECT direction, content FROM messages
		WHERE contact_id = $1 AND type = 'text'
		ORDER BY created_at DESC LIMIT 20
	`, contactID)
	if err != nil {
		return
	}
	defer histRows.Close()

	var history []aiagent.Message
	for histRows.Next() {
		var dir, content string
		histRows.Scan(&dir, &content)
		role := "user"
		if dir == "out" {
			role = "assistant"
		}
		history = append(history, aiagent.Message{Role: role, Content: content})
	}
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	// Injetar catálogo de produtos
	systemPrompt := buildSystemPromptWithProducts(agentPrompt, companyID)
	followupInstruction := "\n\n[INSTRUÇÃO INTERNA: O cliente parou de responder. Envie uma mensagem breve de reativação baseada no contexto da conversa. Não repita o que já foi dito.]"
	systemPrompt += followupInstruction

	cfg := aiagent.Config{Provider: provider, Model: model, APIKey: apiKey}
	aiResp, err := aiagent.Chat(cfg, systemPrompt, history)
	if err != nil {
		log.Printf("[FOLLOWUP] erro AI para contato %s: %v", contactID, err)
		return
	}
	if aiResp == "" {
		return
	}

	// Enviar mensagem
	inst.saveMessage(phone, "", aiResp, "text", "", "out", "", "")

	// Atualizar contagem de follow-ups (last_followup_at já foi atualizado atomicamente acima)
	postgres.DB.Exec(`UPDATE contacts SET followup_count = followup_count + 1 WHERE id = $1`, contactID)

	log.Printf("[FOLLOWUP] mensagem enviada para %s (tentativa %d/%d)", phone, followupCount+1, followupMax)
}
