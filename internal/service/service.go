package service

import (
	"context"
        "log"
	"fmt"
	"math/rand"
	"strings"
	"time"
	"wapi/internal/instance"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// Helper: Criar JID corretamente (grupo ou contato)
func parseJID(number string) types.JID {
	// Se já tem @, fazer parse direto
	if strings.Contains(number, "@") {
		jid, _ := types.ParseJID(number)
		return jid
	}
	
	// Se é número de grupo (mais de 15 dígitos), adicionar @g.us
	if len(number) > 15 {
		return types.NewJID(number, types.GroupServer)
	}
	
	// Caso contrário, é contato normal
	return types.NewJID(number, types.DefaultUserServer)
}

func SendText(inst *instance.Instance, to, message string) error {
	if !inst.WAClient.IsConnected() {
		return fmt.Errorf("instância não conectada")
	}

	jid := parseJID(to)

	inst.WAClient.SendPresence(context.Background(), types.PresenceAvailable)
	time.Sleep(500 * time.Millisecond)

	inst.WAClient.SendChatPresence(context.Background(), jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)

	delay := rand.Intn(inst.TypingDelayMax-inst.TypingDelayMin) + inst.TypingDelayMin
	time.Sleep(time.Duration(delay) * time.Millisecond)

	inst.WAClient.SendChatPresence(context.Background(), jid, types.ChatPresencePaused, types.ChatPresenceMediaText)

	msg := &waProto.Message{
		Conversation: proto.String(message),
	}

	_, err := inst.WAClient.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return fmt.Errorf("erro ao enviar mensagem: %w", err)
	}

	return nil
}

func SendMedia(inst *instance.Instance, to string, data []byte, mimetype, filename, caption string, isAudio bool) error {
	if !inst.WAClient.IsConnected() {
		return fmt.Errorf("instância não conectada")
	}

	jid := parseJID(to)

	inst.WAClient.SendPresence(context.Background(), types.PresenceAvailable)
	time.Sleep(500 * time.Millisecond)

	if isAudio {
		inst.WAClient.SendChatPresence(context.Background(), jid, types.ChatPresenceComposing, types.ChatPresenceMediaAudio)
	} else {
		inst.WAClient.SendChatPresence(context.Background(), jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	}

	delay := rand.Intn(inst.TypingDelayMax-inst.TypingDelayMin) + inst.TypingDelayMin
	time.Sleep(time.Duration(delay) * time.Millisecond)

	uploaded, err := inst.WAClient.Upload(context.Background(), data, whatsmeow.MediaImage)
	if isAudio {
		uploaded, err = inst.WAClient.Upload(context.Background(), data, whatsmeow.MediaAudio)
	} else if mimetype == "image/jpeg" || mimetype == "image/png" || mimetype == "image/webp" {
		uploaded, err = inst.WAClient.Upload(context.Background(), data, whatsmeow.MediaImage)
	} else if strings.HasPrefix(mimetype, "video/") {
		uploaded, err = inst.WAClient.Upload(context.Background(), data, whatsmeow.MediaVideo)
	} else {
		uploaded, err = inst.WAClient.Upload(context.Background(), data, whatsmeow.MediaDocument)
	}

	if err != nil {
        log.Printf("[ERROR] Upload failed - mimetype: %s, isAudio: %v, error: %v", mimetype, isAudio, err)
		return fmt.Errorf("erro ao fazer upload: %w", err)
	}

	var msg *waProto.Message

	if isAudio {
		msg = &waProto.Message{
			AudioMessage: &waProto.AudioMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
				Mimetype:      proto.String("audio/ogg; codecs=opus"),
				PTT:           proto.Bool(true),
			},
		}
	} else if mimetype == "image/jpeg" || mimetype == "image/png" || mimetype == "image/webp" {
		msg = &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
				Mimetype:      proto.String(mimetype),
				Caption:       proto.String(caption),
			},
		}
	} else if strings.HasPrefix(mimetype, "video/") {
		msg = &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
				Mimetype:      proto.String(mimetype),
				Caption:       proto.String(caption),
			},
		}
	} else {
		msg = &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
				Mimetype:      proto.String(mimetype),
				FileName:      proto.String(filename),
				Caption:       proto.String(caption),
			},
		}
	}
        log.Printf("[DEBUG] Sending message - type: %s, size: %d, jid: %s", mimetype, len(data), jid.String())

	_, err = inst.WAClient.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return fmt.Errorf("erro ao enviar mídia: %w", err)
	}

        log.Printf("[DEBUG] Message sent successfully")
	return nil
}

func GetGroups(inst *instance.Instance) ([]map[string]interface{}, error) {
	if !inst.WAClient.IsConnected() {
		return nil, fmt.Errorf("instância não conectada")
	}

	groups, err := inst.WAClient.GetJoinedGroups(context.Background())
	if err != nil {
		return nil, fmt.Errorf("erro ao buscar grupos: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(groups))
	for _, group := range groups {
		result = append(result, map[string]interface{}{
			"id":           group.JID.User, // ID do grupo (sem @g.us)
			"name":         group.Name,
			"participants": len(group.Participants),
		})
	}

	return result, nil
}
