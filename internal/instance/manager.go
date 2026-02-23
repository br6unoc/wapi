package instance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
	"wapi/internal/transcriber"
	"wapi/internal/whatsapp"

	"go.mau.fi/whatsmeow"
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
	list := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		list = append(list, inst)
	}
	return list
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

	client, err := whatsapp.NewClient(id)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("erro ao criar cliente whatsapp: %w", err)
	}

	inst := &Instance{
		ID:             id,
		Name:           name,
		APIKey:         apiKey,
		Status:         "disconnected",
		TypingDelayMin: 1000,
		TypingDelayMax: 3000,
		WAClient:       client,
		ctx:            ctx,
		cancel:         cancel,
		SSEClients:     make(map[chan string]struct{}),
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
	inst.LastQR = ""

	client, err := whatsapp.NewClient(inst.ID)
	if err != nil {
		return fmt.Errorf("erro ao recriar cliente: %w", err)
	}
	inst.WAClient = client

	inst.WAClient.AddEventHandler(func(evt interface{}) {
		inst.handleEvent(evt)
	})

	if inst.WAClient.Store.ID == nil {
		qrChan, err := inst.WAClient.GetQRChannel(inst.ctx)
		if err != nil {
			return fmt.Errorf("erro ao obter canal QR: %w", err)
		}

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

		if err := inst.WAClient.Connect(); err != nil {
			return fmt.Errorf("erro ao conectar: %w", err)
		}

	} else {
		if err := inst.WAClient.Connect(); err != nil {
			return fmt.Errorf("erro ao conectar: %w", err)
		}

		go func() {
			time.Sleep(3 * time.Second)
			if inst.WAClient.IsConnected() {
				inst.Status = "connected"
				if inst.WAClient.Store.ID != nil {
					inst.Phone = inst.WAClient.Store.ID.User
				}
				inst.BroadcastSSE(`{"event":"connected","data":{"phone":"` + inst.Phone + `"}}`)
			}
		}()
	}

	go inst.keepAlive()
	return nil
}

func (inst *Instance) Disconnect() {
	inst.cancel()
	if inst.WAClient != nil {
		inst.WAClient.Logout(context.Background())
		inst.WAClient.Disconnect()
	}
	inst.Status = "disconnected"
	inst.Phone = ""
	inst.LastQR = ""

	ctx, cancel := context.WithCancel(context.Background())
	inst.ctx = ctx
	inst.cancel = cancel
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
					inst.BroadcastSSE(`{"event":"connected","data":{"phone":"` + inst.Phone + `"}}`)
				}
			} else {
				if inst.Status == "connected" {
					inst.Status = "disconnected"
					inst.BroadcastSSE(`{"event":"disconnected","data":{}}`)
				}
			}
		}
	}
}

func (inst *Instance) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		inst.Status = "connected"
		inst.LastQR = ""
		if inst.WAClient.Store.ID != nil {
			inst.Phone = inst.WAClient.Store.ID.User
		}
		inst.BroadcastSSE(`{"event":"connected","data":{"phone":"` + inst.Phone + `"}}`)

	case *events.Disconnected:
		inst.Status = "disconnected"
		inst.BroadcastSSE(`{"event":"disconnected","data":{}}`)

	case *events.Message:
		if v.Info.IsFromMe {
			return
		}
		inst.processMessage(v)
	}
}

func (inst *Instance) processMessage(v *events.Message) {
	msgData := map[string]interface{}{
		"from":      strings.Split(v.Info.Chat.User, "@")[0],
		"pushName":  v.Info.PushName,
		"timestamp": v.Info.Timestamp.Format(time.RFC3339),
		"messageId": v.Info.ID,
		"type":      "text",
		"message":   "",
	}

	if v.Message.GetConversation() != "" {
		msgData["message"] = v.Message.GetConversation()
		msgData["type"] = "text"
	} else if v.Message.GetExtendedTextMessage() != nil {
		msgData["message"] = v.Message.GetExtendedTextMessage().GetText()
		msgData["type"] = "text"
	} else if v.Message.GetImageMessage() != nil {
		msgData["message"] = "[imagem]"
		msgData["type"] = "image"
		if v.Message.GetImageMessage().GetCaption() != "" {
			msgData["message"] = v.Message.GetImageMessage().GetCaption()
		}
	} else if v.Message.GetAudioMessage() != nil {
		msgData["message"] = "[áudio]"
		msgData["type"] = "audio"
		go inst.processAudio(v, msgData)
		return
	} else if v.Message.GetDocumentMessage() != nil {
		msgData["message"] = v.Message.GetDocumentMessage().GetFileName()
		msgData["type"] = "document"
	} else {
		msgData["message"] = "[mensagem não suportada]"
		msgData["type"] = "unknown"
	}

	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)
}

func (inst *Instance) processAudio(v *events.Message, msgData map[string]interface{}) {
	audioMsg := v.Message.GetAudioMessage()

	audioData, err := inst.WAClient.Download(context.Background(), audioMsg)
	if err != nil {
		fmt.Printf("[AUDIO] Erro ao baixar áudio: %v\n", err)
		msgData["transcription"] = ""
		inst.broadcastMessage(msgData)
		go inst.sendWebhook(msgData)
		return
	}

	fmt.Printf("[AUDIO] Áudio baixado: %d bytes\n", len(audioData))
	msgData["message"] = "[áudio]"

	if inst.TranscriptionEnabled {
		fmt.Printf("[AUDIO] Transcrevendo...\n")
		text, err := transcriber.Transcribe(audioData, "audio.ogg")
		if err != nil {
			fmt.Printf("[AUDIO] Erro na transcrição: %v\n", err)
			msgData["transcription"] = ""
		} else {
			fmt.Printf("[AUDIO] Transcrição: %s\n", text)
			msgData["transcription"] = text
		}
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

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(inst.WebhookURL, "application/json", bytes.NewReader(jsonBytes))
	if err != nil {
		fmt.Printf("[WEBHOOK] Erro: %v\n", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("[WEBHOOK] Enviado — status: %d\n", resp.StatusCode)
}

func (inst *Instance) broadcastMessage(msgData map[string]interface{}) {
	payload := map[string]interface{}{
		"event": "message",
		"data":  msgData,
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

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
