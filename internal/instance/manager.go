package instance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
	"wapi/internal/transcriber"
	"wapi/internal/whatsapp"
	"wapi/store/postgres"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type Instance struct {
	ID                   string
	CompanyID            string
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

var (
	Global      = &Manager{instances: make(map[string]*Instance)}
	MessageHook func(companyID string, senderNumber string, message string, inst *Instance)
)

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

func NewInstance(id, companyID, name, apiKey string) (*Instance, error) {
	ctx, cancel := context.WithCancel(context.Background())
	client, container, err := whatsapp.NewClient(id)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("erro ao criar cliente whatsapp: %w", err)
	}
	inst := &Instance{
		ID:                   id,
		CompanyID:            companyID,
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

// Helper: salvar status no banco de dados
func (inst *Instance) saveStatusToDB() {
	postgres.DB.Exec(`UPDATE instances SET status = $1, phone = $2 WHERE id = $3`,
		inst.Status, inst.Phone, inst.ID)
	log.Printf("[DB] Instance %s status saved: %s, phone: %s", inst.Name, inst.Status, inst.Phone)
}

func (inst *Instance) Connect() error {
	// Limpa estado anterior para garantir fresh start
	if inst.WAClient != nil {
		inst.WAClient.Disconnect()
	}
	inst.cancel()
	inst.LastQR = "" // Limpa o QR anterior para não mostrar lixo no frontend

	ctx, cancel := context.WithCancel(context.Background())
	inst.ctx = ctx
	inst.cancel = cancel

	client, container, err := whatsapp.NewClient(inst.Phone)
	if err != nil {
		return err
	}
	inst.WAClient = client
	inst.Container = container
	inst.WAClient.AddEventHandler(inst.handleEvent)

	if inst.WAClient.Store.ID == nil {
		qrChan, _ := inst.WAClient.GetQRChannel(inst.ctx)
		go func() {
			for {
				select {
				case <-inst.ctx.Done():
					return
				case evt, ok := <-qrChan:
					if !ok {
						return
					}
					if evt.Event == "code" {
						inst.LastQR = evt.Code
						inst.BroadcastSSE(`{"event":"qr","data":{"qrcode":"` + evt.Code + `"}}`)
					}
				}
			}
		}()
	}

	log.Printf("[CONNECT] Chamando Connect() para instância %s...", inst.Name)
	inst.Status = "connecting"
	inst.BroadcastSSE(`{"event":"connecting","data":{}}`)
	inst.saveStatusToDB()

	err = inst.WAClient.Connect()
	if err != nil {
		log.Printf("[CONNECT] Erro ao conectar instância %s: %v", inst.Name, err)
		inst.Status = "disconnected"
		inst.BroadcastSSE(`{"event":"disconnected","data":{}}`)
		inst.saveStatusToDB()
	} else {
		log.Printf("[CONNECT] Connect() chamado com sucesso para %s (aguardando evento Connected)", inst.Name)
	}

	go inst.keepAlive()
	return err
}

func (inst *Instance) Disconnect() {
	inst.cancel()
	if inst.WAClient != nil {
		inst.WAClient.Disconnect()
	}
	inst.Status = "disconnected"
	inst.Phone = ""
	inst.LastQR = ""
	inst.saveStatusToDB()
}

func (inst *Instance) Logout() error {
	if inst.WAClient != nil && inst.WAClient.IsConnected() {
		err := inst.WAClient.Logout(context.Background())
		if err != nil {
			log.Printf("[LOGOUT] Erro ao fazer logout no WhatsApp: %v", err)
		}
	}
	inst.Disconnect()
	return nil
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
					if inst.WAClient.Store.ID != nil && inst.Status != "connected" {
						inst.Status = "connected"
						inst.Phone = inst.WAClient.Store.ID.User
						log.Printf("[KEEPALIVE] Instance %s reconnected - Phone: %s", inst.Name, inst.Phone)
						inst.BroadcastSSE(fmt.Sprintf(`{"event":"connected","data":{"phone":"%s"}}`, inst.Phone))
						inst.saveStatusToDB()
					}
			} else {
				if inst.Status == "connected" {
					inst.Status = "disconnected"
					inst.Phone = ""
					log.Printf("[KEEPALIVE] Instance %s disconnected", inst.Name)
					inst.BroadcastSSE(`{"event":"disconnected","data":{}}`)
					inst.saveStatusToDB()
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
		inst.BroadcastSSE(fmt.Sprintf(`{"event":"connected","data":{"phone":"%s","qrcode":""}}`, inst.Phone))
		inst.saveStatusToDB()
	case *events.Disconnected:
		inst.Status = "disconnected"
		inst.Phone = ""
		log.Printf("[EVENT] Instance %s Disconnected", inst.Name)
		inst.BroadcastSSE(`{"event":"disconnected","data":{}}`)
		inst.saveStatusToDB()
	case *events.LoggedOut:
		inst.Status = "disconnected"
		inst.Phone = ""
		inst.LastQR = ""
		log.Printf("[EVENT] Instance %s Logged Out from Phone", inst.Name)
		inst.BroadcastSSE(`{"event":"disconnected","reason":"logged_out","data":{}}`)
		inst.saveStatusToDB()
	case *events.Message:
		if v.Info.IsFromMe {
			return
		}
		inst.processMessage(v)
	}
}

func (inst *Instance) processMessage(v *events.Message) {
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

	if v.Message.GetConversation() != "" {
		msgData["message"] = v.Message.GetConversation()
	} else if v.Message.GetExtendedTextMessage() != nil {
		msgData["message"] = v.Message.GetExtendedTextMessage().GetText()
	} else if v.Message.GetImageMessage() != nil {
		msgData["message"] = "[imagem]"
		if v.Message.GetImageMessage().GetCaption() != "" {
			msgData["message"] = v.Message.GetImageMessage().GetCaption()
		}
	} else if v.Message.GetAudioMessage() != nil {
		msgData["message"] = "[áudio]"
		msgData["type"] = "audio"
		go inst.processAudio(v, msgData)
		return
	}

	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)

	// SDR Nativo: Dispara o hook se estiver definido e não for grupo
	if MessageHook != nil && !isGroup && msgData["message"].(string) != "" {
		go MessageHook(inst.CompanyID, senderNumber, msgData["message"].(string), inst)
	}
}

func (inst *Instance) processAudio(v *events.Message, msgData map[string]interface{}) {
	log.Printf("[AUDIO] Inciando processamento de áudio para %s", msgData["sender_number"])
	audioMsg := v.Message.GetAudioMessage()
	
	// whatsmeow 0.2.x uses Download(ctx, msg) or Download(msg). We will add error logging.
	audioData, err := inst.WAClient.Download(context.Background(), audioMsg)
	if err != nil {
		log.Printf("[AUDIO-ERROR] Falha no download do whatsmeow: %v", err)
		inst.broadcastMessage(msgData)
		go inst.sendWebhook(msgData)
		return
	}
	
	log.Printf("[AUDIO] Download concluído, %d bytes. TranscriptionEnabled: %v", len(audioData), inst.TranscriptionEnabled)
	if inst.TranscriptionEnabled {
		text, err := transcriber.Transcribe(audioData, "audio.ogg")
		if err != nil {
			log.Printf("[AUDIO-ERROR] Erro na transcrição: %v", err)
		} else {
			log.Printf("[AUDIO] Transcrição bem sucedida: %s", text)
			msgData["transcription"] = text
		}
	}
	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)

	// SDR Nativo para Áudio Transcrito
	if MessageHook != nil && msgData["transcription"] != nil && msgData["transcription"].(string) != "" {
		go MessageHook(inst.CompanyID, msgData["sender_number"].(string), msgData["transcription"].(string), inst)
	}
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
	resp, err := http.Post(inst.WebhookURL, "application/json", bytes.NewReader(jsonBytes))
	if err != nil {
		log.Printf("[WEBHOOK-ERROR] Instance %s failed to send to %s: %v", inst.Name, inst.WebhookURL, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("[WEBHOOK-ERROR] Instance %s received status %d from %s", inst.Name, resp.StatusCode, inst.WebhookURL)
	}
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

func (inst *Instance) Ctx() <-chan struct{} {
	return inst.ctx.Done()
}
