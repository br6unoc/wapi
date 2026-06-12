package instance

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"botwapp/internal/aiagent"
	"botwapp/internal/hub"
	"botwapp/internal/whatsapp"
	"botwapp/store/postgres"

	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type Instance struct {
	ID                   string
	Name                 string
	APIKey               string
	WebhookURL           string
	TranscriptionEnabled bool
	TypingDelayMin       int
	TypingDelayMax       int
	Status               string
	Phone                string
	LastQR               string
	WAClient             *whatsmeow.Client
	Container            *sqlstore.Container
	ctx                  context.Context
	cancel               context.CancelFunc
	SSEClients           map[chan string]struct{}
	sseMu                sync.Mutex
}

type Manager struct {
	instances map[string]*Instance
	mu        sync.RWMutex
}

var Global = &Manager{
	instances: make(map[string]*Instance),
}

func (m *Manager) Add(inst *Instance) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instances[inst.ID] = inst
}

func (m *Manager) Get(id string) (*Instance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.instances[id]
	return inst, ok
}

func (m *Manager) GetByName(name string) (*Instance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, inst := range m.instances {
		if inst.Name == name {
			return inst, true
		}
	}
	return nil, false
}

func (m *Manager) GetAll() []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		all = append(all, inst)
	}
	return all
}

func (m *Manager) DisconnectAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, inst := range m.instances {
		inst.cancel()
		if inst.WAClient != nil {
			inst.WAClient.Disconnect()
		}
		log.Printf("[SHUTDOWN] Instância %s desconectada.", inst.Name)
	}
}

func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if inst, ok := m.instances[id]; ok {
		inst.cancel()
		if inst.WAClient != nil {
			inst.WAClient.Disconnect()
		}
		delete(m.instances, id)
	}
}

func NewInstance(id, name, apiKey string) (*Instance, error) {
	ctx, cancel := context.WithCancel(context.Background())
	client, container, err := whatsapp.NewClient(id)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("erro ao criar cliente whatsapp: %w", err)
	}
	inst := &Instance{
		ID:                   id,
		Name:                 name,
		APIKey:               apiKey,
		Status:               "disconnected",
		TranscriptionEnabled: true,
		TypingDelayMin:       1000,
		TypingDelayMax:       3000,
		WAClient:             client,
		Container:            container,
		ctx:                  ctx,
		cancel:               cancel,
		SSEClients:           make(map[chan string]struct{}),
	}
	return inst, nil
}

// SaveStatusToDB salva o status atual no banco (exportado para uso externo)
func (inst *Instance) SaveStatusToDB() {
	inst.saveStatusToDB()
}

// Helper: salvar status no banco de dados
func (inst *Instance) saveStatusToDB() {
	postgres.DB.Exec(`UPDATE instances SET status = $1, phone = $2 WHERE id = $3`,
		inst.Status, inst.Phone, inst.ID)
	log.Printf("[DB] Instance %s status saved: %s, phone: %s", inst.Name, inst.Status, inst.Phone)
}

func (inst *Instance) Connect() error {
	if inst.WAClient != nil {
		inst.WAClient.Disconnect()
	}
	inst.cancel()
	ctx, cancel := context.WithCancel(context.Background())
	inst.ctx = ctx
	inst.cancel = cancel

	client, container, err := whatsapp.NewClient(inst.ID)
	if err != nil {
		return err
	}
	inst.WAClient = client
	inst.Container = container
	inst.WAClient.AddEventHandler(inst.handleEvent)

	if inst.WAClient.Store.ID == nil {
		qrChan, err := inst.WAClient.GetQRChannel(inst.ctx)
		if err != nil {
			log.Printf("[QR] Instance %s GetQRChannel error: %v", inst.Name, err)
		} else {
			go func() {
				for {
					select {
					case <-inst.ctx.Done():
						return
					case evt, ok := <-qrChan:
						if !ok {
							log.Printf("[QR] Instance %s QR channel closed", inst.Name)
							return
						}
						switch evt.Event {
						case "code":
							inst.LastQR = evt.Code
							log.Printf("[QR] Instance %s got QR code (len=%d)", inst.Name, len(evt.Code))
							dataURL := QRToDataURL(evt.Code)
							payload, _ := json.Marshal(map[string]interface{}{
								"event": "qr",
								"data":  map[string]string{"qrcode": dataURL},
							})
							inst.BroadcastSSE(string(payload))
						case "success":
							log.Printf("[QR] Instance %s QR scanned successfully, waiting for Connected event", inst.Name)
						case "timeout":
							log.Printf("[QR] Instance %s QR timeout", inst.Name)
						default:
							log.Printf("[QR] Instance %s QR event: %s (err: %v)", inst.Name, evt.Event, evt.Error)
						}
					}
				}
			}()
		}
	}

	err = inst.WAClient.Connect()

	// Verificar e atualizar status após Connect
	go func() {
		time.Sleep(3 * time.Second)
		if inst.WAClient.IsConnected() && inst.WAClient.Store.ID != nil {
			inst.Status = "connected"
			inst.Phone = inst.WAClient.Store.ID.User
			log.Printf("[CONNECT] Instance %s connected - Phone: %s", inst.Name, inst.Phone)
			p, _ := json.Marshal(map[string]interface{}{"event": "connected", "data": map[string]string{"phone": inst.Phone}})
			inst.BroadcastSSE(string(p))
			inst.saveStatusToDB()
		} else {
			inst.Status = "disconnected"
			log.Printf("[CONNECT] Instance %s not authenticated yet (waiting for QR scan)", inst.Name)
			inst.saveStatusToDB()
		}
	}()

	go inst.keepAlive()
	return err
}

// Disconnect faz logout completo do WhatsApp (revoga a sessão no celular),
// apaga o arquivo de sessão local e para o autoReconnect.
// Na próxima vez que o usuário clicar em Conectar, um novo QR será gerado.
func (inst *Instance) Disconnect() {
	if inst.WAClient != nil {
		// Logout revoga a sessão no servidor do WA (remove o dispositivo vinculado)
		_ = inst.WAClient.Logout(context.Background())
		inst.WAClient.Disconnect()
	}

	// Apaga o arquivo de sessão para garantir novo QR no próximo Connect
	sessionFile := fmt.Sprintf("/app/sessions/%s.db", inst.ID)
	os.Remove(sessionFile)
	log.Printf("[DISCONNECT] Sessão de %s revogada e apagada", inst.Name)

	inst.cancel()
	inst.Status = "disconnected"
	inst.Phone = ""
	inst.LastQR = ""
	inst.saveStatusToDB()
}

func (inst *Instance) autoReconnect() {
	// Sem sessão salva não há o que reconectar — aguarda ação do usuário
	if inst.WAClient == nil || inst.WAClient.Store.ID == nil {
		log.Printf("[RECONNECT] Instância %s sem sessão válida, aguardando novo QR", inst.Name)
		return
	}

	for attempt := 1; attempt <= 3; attempt++ {
		select {
		case <-inst.ctx.Done():
			return
		case <-time.After(time.Duration(attempt*30) * time.Second):
		}
		if inst.Status == "connected" {
			return
		}
		// Reconfirma: sessão pode ter sido apagada enquanto aguardava
		if inst.WAClient == nil || inst.WAClient.Store.ID == nil {
			log.Printf("[RECONNECT] Sessão de %s foi removida, cancelando reconexão", inst.Name)
			return
		}
		log.Printf("[RECONNECT] Tentativa %d/3 para instância %s", attempt, inst.Name)
		if err := inst.Connect(); err != nil {
			log.Printf("[RECONNECT] Erro na tentativa %d para %s: %v", attempt, inst.Name, err)
			continue
		}
		time.Sleep(5 * time.Second)
		if inst.Status == "connected" {
			log.Printf("[RECONNECT] Instância %s reconectada na tentativa %d", inst.Name, attempt)
			return
		}
	}
	log.Printf("[RECONNECT] Instância %s não reconectou após 3 tentativas", inst.Name)
}

func (inst *Instance) keepAlive() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-inst.ctx.Done():
			return
		case <-ticker.C:
			if inst.WAClient.IsConnected() {
				inst.WAClient.SendPresence(context.Background(), types.PresenceAvailable)
				if inst.Status != "connected" && inst.WAClient.Store.ID != nil {
					inst.Status = "connected"
					inst.Phone = inst.WAClient.Store.ID.User
					log.Printf("[KEEPALIVE] Instance %s reconnected - Phone: %s", inst.Name, inst.Phone)
					p, _ := json.Marshal(map[string]interface{}{"event": "connected", "data": map[string]string{"phone": inst.Phone}})
					inst.BroadcastSSE(string(p))
					inst.saveStatusToDB()
				}
			} else {
				if inst.Status == "connected" {
					inst.Status = "disconnected"
					inst.Phone = ""
					log.Printf("[KEEPALIVE] Instance %s disconnected, iniciando auto-reconexão...", inst.Name)
					p, _ := json.Marshal(map[string]interface{}{"event": "disconnected", "data": map[string]interface{}{}})
					inst.BroadcastSSE(string(p))
					inst.saveStatusToDB()
					go inst.autoReconnect()
				}
			}
		}
	}
}

func (inst *Instance) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		inst.Status = "connected"
		if inst.WAClient.Store.ID != nil {
			inst.Phone = inst.WAClient.Store.ID.User
		}
		log.Printf("[EVENT] Instance %s Connected - Phone: %s", inst.Name, inst.Phone)
		p, _ := json.Marshal(map[string]interface{}{"event": "connected", "data": map[string]string{"phone": inst.Phone, "qrcode": ""}})
		inst.BroadcastSSE(string(p))
		inst.saveStatusToDB()
	case *events.Disconnected:
		inst.Status = "disconnected"
		inst.Phone = ""
		log.Printf("[EVENT] Instance %s Disconnected", inst.Name)
		inst.BroadcastSSE(`{"event":"disconnected","data":{}}`)
		inst.saveStatusToDB()
	case *events.LoggedOut:
		log.Printf("[EVENT] Instance %s LoggedOut — removido pelo celular/app", inst.Name)
		inst.Status = "disconnected"
		inst.Phone = ""
		inst.LastQR = ""
		inst.BroadcastSSE(`{"event":"disconnected","data":{"reason":"logged_out"}}`)
		inst.saveStatusToDB()
		os.Remove(fmt.Sprintf("/app/sessions/%s.db", inst.ID))
	case *events.Message:
		if v.Info.IsFromMe {
			if v.Info.Chat.Server != "g.us" && v.Info.Chat.Server != "broadcast" {
				go inst.processOutgoingMessage(v)
			}
			return
		}
		inst.processMessage(v)
	}
}

func (inst *Instance) processMessage(v *events.Message) {
	// Ignorar status/stories do WhatsApp (status@broadcast)
	if v.Info.Chat.Server == "broadcast" || v.Info.Chat.User == "status" {
		return
	}

	isGroup := v.Info.Chat.Server == "g.us"
	remoteJID := v.Info.Chat.User

	var senderNumber string
	if v.Info.Sender.Server == "lid" {
		phoneJID, err := inst.Container.LIDMap.GetPNForLID(context.Background(), v.Info.Sender)
		if err == nil && !phoneJID.IsEmpty() {
			senderNumber = phoneJID.User
			log.Printf("[LID RESOLVED] %s → %s", v.Info.Sender.User, phoneJID.User)
		} else {
			senderNumber = v.Info.Sender.User
			log.Printf("[LID NOT FOUND] %s (erro: %v)", v.Info.Sender.User, err)
		}
	} else {
		senderNumber = v.Info.Sender.ToNonAD().User
	}

	log.Printf("[MESSAGE] remote_jid=%s, sender=%s, isGroup=%v, pushName=%s",
		remoteJID, senderNumber, isGroup, v.Info.PushName)

	msgData := map[string]interface{}{
		"remote_jid":    remoteJID,
		"sender_number": senderNumber,
		"pushName":      v.Info.PushName,
		"is_group":      isGroup,
		"timestamp":     v.Info.Timestamp.Format(time.RFC3339),
		"messageId":     v.Info.ID,
		"type":          "text",
		"message":       "",
	}

	content := ""
	msgTypeStr := "text"

	if v.Message.GetConversation() != "" {
		content = v.Message.GetConversation()
		msgData["message"] = content
	} else if v.Message.GetExtendedTextMessage() != nil {
		content = v.Message.GetExtendedTextMessage().GetText()
		msgData["message"] = content
	} else if v.Message.GetImageMessage() != nil {
		msgData["message"] = "[imagem]"
		msgData["type"] = "image"
		go inst.processMedia(v, senderNumber, isGroup, msgData, "image", v.Message.GetImageMessage().GetCaption())
		return
	} else if v.Message.GetVideoMessage() != nil {
		msgData["message"] = "[vídeo]"
		msgData["type"] = "video"
		go inst.processMedia(v, senderNumber, isGroup, msgData, "video", v.Message.GetVideoMessage().GetCaption())
		return
	} else if v.Message.GetDocumentMessage() != nil {
		msgData["message"] = "[documento]"
		msgData["type"] = "document"
		go inst.processMedia(v, senderNumber, isGroup, msgData, "document", v.Message.GetDocumentMessage().GetFileName())
		return
	} else if v.Message.GetAudioMessage() != nil {
		msgData["message"] = "[áudio]"
		msgData["type"] = "audio"
		go inst.processAudio(v, senderNumber, isGroup, msgData)
		return
	}

	if !isGroup {
		if msgTypeStr == "text" {
			go inst.saveMessageAndReply(senderNumber, v.Info.PushName, content, v.Info.ID)
		} else {
			go inst.saveMessage(senderNumber, v.Info.PushName, content, msgTypeStr, v.Info.ID, "in", "", "")
		}
	}

	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)
}

// processOutgoingMessage salva mensagens enviadas pelo celular físico (IsFromMe).
func (inst *Instance) processOutgoingMessage(v *events.Message) {
	var phone string
	chatJID := v.Info.Chat.ToNonAD()
	if chatJID.Server == "lid" {
		// Resolve LID → número de telefone real
		phoneJID, err := inst.Container.LIDMap.GetPNForLID(context.Background(), chatJID)
		if err == nil && !phoneJID.IsEmpty() {
			phone = phoneJID.User
		} else {
			log.Printf("[OUTGOING] LID não resolvido: %s (%v)", chatJID.User, err)
			return
		}
	} else {
		phone = chatJID.User
	}
	if phone == "" {
		return
	}

	var content, msgType string
	switch {
	case v.Message.GetConversation() != "":
		content = v.Message.GetConversation()
		msgType = "text"
	case v.Message.GetExtendedTextMessage() != nil:
		content = v.Message.GetExtendedTextMessage().GetText()
		msgType = "text"
	case v.Message.GetImageMessage() != nil:
		content = "[imagem]"
		msgType = "image"
	case v.Message.GetVideoMessage() != nil:
		content = "[vídeo]"
		msgType = "video"
	case v.Message.GetDocumentMessage() != nil:
		content = "[documento]"
		msgType = "document"
	case v.Message.GetAudioMessage() != nil:
		content = "[áudio]"
		msgType = "audio"
	default:
		return
	}

	inst.saveMessage(phone, "", content, msgType, v.Info.ID, "out", "", "")
}

func (inst *Instance) processAudio(v *events.Message, senderNumber string, isGroup bool, msgData map[string]interface{}) {
	audioMsg := v.Message.GetAudioMessage()
	audioData, err := inst.WAClient.Download(context.Background(), audioMsg)
	if err != nil {
		log.Printf("[AUDIO] Erro ao baixar áudio: %v", err)
		if !isGroup {
			go inst.saveMessage(senderNumber, v.Info.PushName, "[áudio]", "audio", v.Info.ID, "in", "", "")
		}
		inst.broadcastMessage(msgData)
		go inst.sendWebhook(msgData)
		return
	}

	// Salva o arquivo de áudio em disco
	mediaDir := "/app/media/audio"
	os.MkdirAll(mediaDir, 0755)
	mediaPath := fmt.Sprintf("%s/%s.ogg", mediaDir, v.Info.ID)
	if err := os.WriteFile(mediaPath, audioData, 0644); err != nil {
		log.Printf("[AUDIO] Erro ao salvar arquivo: %v", err)
		mediaPath = ""
	}
	webPath := ""
	if mediaPath != "" {
		webPath = fmt.Sprintf("/media/audio/%s.ogg", v.Info.ID)
		msgData["media_path"] = webPath
	}

	if !isGroup {
		go inst.saveMessage(senderNumber, v.Info.PushName, "[áudio]", "audio", v.Info.ID, "in", webPath, "")
	}
	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)
}

func (inst *Instance) processMedia(v *events.Message, senderNumber string, isGroup bool, msgData map[string]interface{}, mediaType, caption string) {
	var (
		data      []byte
		err       error
		ext       string
		mediaName string
	)

	switch mediaType {
	case "image":
		img := v.Message.GetImageMessage()
		data, err = inst.WAClient.Download(context.Background(), img)
		mime := img.GetMimetype()
		switch mime {
		case "image/png":
			ext = "png"
		case "image/webp":
			ext = "webp"
		default:
			ext = "jpg"
		}
	case "video":
		data, err = inst.WAClient.Download(context.Background(), v.Message.GetVideoMessage())
		ext = "mp4"
	case "document":
		doc := v.Message.GetDocumentMessage()
		data, err = inst.WAClient.Download(context.Background(), doc)
		mediaName = doc.GetFileName()
		if mediaName == "" {
			mediaName = "documento"
		}
		ext = "bin"
		if n := mediaName; len(n) > 0 {
			if idx := len(n) - 1; idx >= 0 {
				for i := len(n) - 1; i >= 0; i-- {
					if n[i] == '.' {
						ext = n[i+1:]
						break
					}
				}
			}
		}
	}

	content := caption
	if content == "" {
		switch mediaType {
		case "image":
			content = "[imagem]"
		case "video":
			content = "[vídeo]"
		case "document":
			content = "[documento]"
		}
	}

	if err != nil {
		log.Printf("[MEDIA] Erro ao baixar %s: %v", mediaType, err)
		if !isGroup {
			go inst.saveMessage(senderNumber, v.Info.PushName, content, mediaType, v.Info.ID, "in", "", mediaName)
		}
		inst.broadcastMessage(msgData)
		go inst.sendWebhook(msgData)
		return
	}

	mediaDir := fmt.Sprintf("/app/media/%ss", mediaType)
	os.MkdirAll(mediaDir, 0755)
	fsPath := fmt.Sprintf("%s/%s.%s", mediaDir, v.Info.ID, ext)
	webPath := ""
	if err := os.WriteFile(fsPath, data, 0644); err != nil {
		log.Printf("[MEDIA] Erro ao salvar %s: %v", mediaType, err)
	} else {
		webPath = fmt.Sprintf("/media/%ss/%s.%s", mediaType, v.Info.ID, ext)
		msgData["media_path"] = webPath
		msgData["media_name"] = mediaName
	}

	msgData["message"] = content

	if !isGroup {
		go inst.saveMessage(senderNumber, v.Info.PushName, content, mediaType, v.Info.ID, "in", webPath, mediaName)
	}
	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)
}

func (inst *Instance) sendWebhook(msgData map[string]interface{}) {
	if inst.WebhookURL == "" {
		return
	}
	payload := map[string]interface{}{
		"instance":   inst.Name,
		"instanceId": inst.ID,
		"event":      "messages.upsert",
		"data":       msgData,
	}
	jsonBytes, _ := json.Marshal(payload)
	http.Post(inst.WebhookURL, "application/json", bytes.NewReader(jsonBytes))
}

func (inst *Instance) broadcastMessage(msgData map[string]interface{}) {
	payload := map[string]interface{}{"event": "message", "data": msgData}
	jsonBytes, _ := json.Marshal(payload)
	inst.BroadcastSSE(string(jsonBytes))
}

func (inst *Instance) BroadcastSSE(msg string) {
	inst.sseMu.Lock()
	defer inst.sseMu.Unlock()
	for ch := range inst.SSEClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (inst *Instance) AddSSEClient(ch chan string) {
	inst.sseMu.Lock()
	defer inst.sseMu.Unlock()
	inst.SSEClients[ch] = struct{}{}
}

func (inst *Instance) RemoveSSEClient(ch chan string) {
	inst.sseMu.Lock()
	defer inst.sseMu.Unlock()
	delete(inst.SSEClients, ch)
}

// saveMessageAndReply salva mensagem de texto e tenta resposta do agente.
func (inst *Instance) saveMessageAndReply(phone, pushName, content, msgID string) {
	inst.saveMessage(phone, pushName, content, "text", msgID, "in", "", "")

	var contactID string
	err := postgres.DB.QueryRow(
		`SELECT id FROM contacts WHERE phone = $1 AND instance_id = $2`,
		phone, inst.ID,
	).Scan(&contactID)
	if err != nil {
		log.Printf("[AGENT] erro ao buscar contato %s: %v", phone, err)
		return
	}

	inst.tryAgentReply(contactID, phone)
}

// tryAgentReply verifica se há agente ativo e envia resposta automática via AI.
func (inst *Instance) tryAgentReply(contactID, phone string) {
	// 1. Verificar agent_mode e is_first_contact
	var agentMode, isFirstContact bool
	err := postgres.DB.QueryRow(
		`SELECT agent_mode, is_first_contact FROM contacts WHERE id = $1`,
		contactID,
	).Scan(&agentMode, &isFirstContact)
	if err != nil || !agentMode {
		return
	}

	// 2. Buscar agent_id e company_id desta instância numa única query
	var firstAgentID, returningAgentID sql.NullString
	var companyID string
	err = postgres.DB.QueryRow(
		`SELECT COALESCE(company_id::text,''), first_contact_agent_id, returning_agent_id FROM instances WHERE id = $1`,
		inst.ID,
	).Scan(&companyID, &firstAgentID, &returningAgentID)
	if err != nil || companyID == "" {
		log.Printf("[AGENT] instância %s sem company_id: %v", inst.ID, err)
		return
	}

	var agentID string
	if isFirstContact {
		if !firstAgentID.Valid || firstAgentID.String == "" {
			return
		}
		agentID = firstAgentID.String
	} else {
		if !returningAgentID.Valid || returningAgentID.String == "" {
			return
		}
		agentID = returningAgentID.String
	}

	// 3. Buscar dados do agente
	var agentPrompt, handoffKeyword string
	err = postgres.DB.QueryRow(
		`SELECT prompt, handoff_keyword FROM agents WHERE id = $1 AND is_active = TRUE`,
		agentID,
	).Scan(&agentPrompt, &handoffKeyword)
	if err != nil {
		return
	}

	// 4. Carregar configuração de AI conversacional desta empresa
	provider, _ := postgres.GetCompanySetting(companyID, "conversational_ai_provider")
	model, _ := postgres.GetCompanySetting(companyID, "conversational_ai_model")
	apiKey, _ := postgres.GetCompanySetting(companyID, "conversational_ai_api_key")
	// Fallback para chaves legadas
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
		log.Printf("[AGENT] configuração de AI conversacional incompleta para empresa %s", companyID)
		return
	}

	// 5. Carregar últimas 20 mensagens de texto deste contato como histórico
	rows, err := postgres.DB.Query(
		`SELECT direction, content FROM messages WHERE contact_id = $1 AND type = 'text' ORDER BY created_at DESC LIMIT 20`,
		contactID,
	)
	if err != nil {
		log.Printf("[AGENT] erro ao buscar histórico: %v", err)
		return
	}
	defer rows.Close()

	var history []aiagent.Message
	for rows.Next() {
		var dir, msgContent string
		rows.Scan(&dir, &msgContent)
		role := "user"
		if dir == "out" {
			role = "assistant"
		}
		history = append(history, aiagent.Message{Role: role, Content: msgContent})
	}
	// Inverter: do mais antigo para o mais recente
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	// 6. Chamar AI
	cfg := aiagent.Config{Provider: provider, Model: model, APIKey: apiKey}
	aiResp, err := aiagent.Chat(cfg, agentPrompt, history)
	if err != nil {
		log.Printf("[AGENT] erro na chamada AI: %v", err)
		return
	}

	// 7. Verificar palavra-chave de handoff
	if handoffKeyword != "" && strings.Contains(aiResp, handoffKeyword) {
		postgres.DB.Exec(`UPDATE contacts SET agent_mode = FALSE WHERE id = $1`, contactID)
		aiResp = strings.TrimSpace(strings.ReplaceAll(aiResp, handoffKeyword, ""))
		payload, _ := json.Marshal(map[string]interface{}{
			"event": "agent_mode_changed",
			"data":  map[string]interface{}{"contact_id": contactID, "agent_mode": false},
		})
		hub.Global.Broadcast(payload)
	}

	if aiResp == "" {
		return
	}

	// 8. Marcar is_first_contact = false
	postgres.DB.Exec(`UPDATE contacts SET is_first_contact = FALSE WHERE id = $1`, contactID)

	// 9. Salvar resposta da AI como mensagem de saída
	outID := uuid.New().String()
	var createdAt string
	postgres.DB.QueryRow(
		`INSERT INTO messages (id, instance_id, contact_id, direction, content, type, wa_message_id, media_path, media_name)
		 VALUES ($1, $2, $3, 'out', $4, 'text', '', '', '') RETURNING created_at`,
		outID, inst.ID, contactID, aiResp,
	).Scan(&createdAt)

	// 10. Broadcast evento WebSocket new_message
	wsPayload, _ := json.Marshal(map[string]interface{}{
		"event": "new_message",
		"data": map[string]interface{}{
			"id":            outID,
			"instance_id":   inst.ID,
			"instance_name": inst.Name,
			"contact_id":    contactID,
			"phone":         phone,
			"direction":     "out",
			"content":       aiResp,
			"type":          "text",
			"media_path":    "",
			"media_name":    "",
			"created_at":    createdAt,
		},
	})
	hub.Global.Broadcast(wsPayload)

	// 11. Enviar via WhatsApp
	jid := types.NewJID(phone, types.DefaultUserServer)
	msg := &waProto.Message{
		Conversation: proto.String(aiResp),
	}
	if _, err := inst.WAClient.SendMessage(context.Background(), jid, msg); err != nil {
		log.Printf("[AGENT] erro ao enviar mensagem WhatsApp: %v", err)
	}
}

// saveMessage persiste a mensagem no banco e faz broadcast via WebSocket.
func (inst *Instance) saveMessage(phone, pushName, content, msgType, waID, direction, mediaPath, mediaName string) {
	var contactID string
	err := postgres.DB.QueryRow(
		`INSERT INTO contacts (instance_id, phone, name, last_contact_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (instance_id, phone) DO UPDATE
		   SET name = EXCLUDED.name, last_contact_at = NOW()
		 RETURNING id`,
		inst.ID, phone, pushName,
	).Scan(&contactID)
	if err != nil {
		log.Printf("[DB] Erro ao upsert contact: %v", err)
		return
	}

	msgID := uuid.New().String()
	var createdAt string
	err = postgres.DB.QueryRow(
		`INSERT INTO messages (id, instance_id, contact_id, direction, content, type, wa_message_id, media_path, media_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING created_at`,
		msgID, inst.ID, contactID, direction, content, msgType, waID, mediaPath, mediaName,
	).Scan(&createdAt)
	if err != nil {
		log.Printf("[DB] Erro ao salvar mensagem: %v", err)
		return
	}

	if direction == "in" {
		postgres.DB.Exec(
			`UPDATE contacts SET unread_count = unread_count + 1 WHERE id = $1`,
			contactID,
		)
	}

	payload := map[string]interface{}{
		"event": "new_message",
		"data": map[string]interface{}{
			"id":            msgID,
			"instance_id":   inst.ID,
			"instance_name": inst.Name,
			"contact_id":    contactID,
			"phone":         phone,
			"name":          pushName,
			"direction":     direction,
			"content":       content,
			"type":          msgType,
			"media_path":    mediaPath,
			"media_name":    mediaName,
			"created_at":    createdAt,
		},
	}
	jsonBytes, _ := json.Marshal(payload)
	hub.Global.Broadcast(jsonBytes)
}

func (inst *Instance) Ctx() <-chan struct{} {
	return inst.ctx.Done()
}

func QRToDataURL(content string) string {
	png, err := qrcode.Encode(content, qrcode.Low, 256)
	if err != nil {
		log.Printf("[QR] Erro ao gerar QR PNG: %v", err)
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}
