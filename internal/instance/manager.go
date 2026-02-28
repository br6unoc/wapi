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

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
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

	err = inst.WAClient.Connect()
	
	// Verificar e atualizar status após Connect
	go func() {
		time.Sleep(3 * time.Second)
		if inst.WAClient.IsConnected() {
			inst.Status = "connected"
			if inst.WAClient.Store.ID != nil {
				inst.Phone = inst.WAClient.Store.ID.User
			}
			log.Printf("[CONNECT] Instance %s connected - Phone: %s", inst.Name, inst.Phone)
		} else {
			inst.Status = "disconnected"
			log.Printf("[CONNECT] Instance %s failed to connect", inst.Name)
		}
	}()
	
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
				if inst.Status != "connected" {
					inst.Status = "connected"
					if inst.WAClient.Store.ID != nil {
						inst.Phone = inst.WAClient.Store.ID.User
					}
					log.Printf("[KEEPALIVE] Instance %s reconnected - Phone: %s", inst.Name, inst.Phone)
				}
			} else {
				if inst.Status == "connected" {
					inst.Status = "disconnected"
					inst.Phone = ""
					log.Printf("[KEEPALIVE] Instance %s disconnected", inst.Name)
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
	case *events.Disconnected:
		inst.Status = "disconnected"
		inst.Phone = ""
		log.Printf("[EVENT] Instance %s Disconnected", inst.Name)
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
}

func (inst *Instance) processAudio(v *events.Message, msgData map[string]interface{}) {
	audioMsg := v.Message.GetAudioMessage()
	audioData, err := inst.WAClient.Download(context.Background(), audioMsg)
	if err != nil {
		inst.broadcastMessage(msgData)
		go inst.sendWebhook(msgData)
		return
	}
	if inst.TranscriptionEnabled {
		text, _ := transcriber.Transcribe(audioData, "audio.ogg")
		msgData["transcription"] = text
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

func (inst *Instance) Ctx() <-chan struct{} {
	return inst.ctx.Done()
}
