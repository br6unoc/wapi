package service

import (
	"context"
	"fmt"
	"math/rand"
	"time"
	"wapi/internal/instance"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func SendText(inst *instance.Instance, to, message string) error {
	if !inst.WAClient.IsConnected() {
		return fmt.Errorf("instância não conectada")
	}

	jid := types.NewJID(to, types.DefaultUserServer)

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

	jid := types.NewJID(to, types.DefaultUserServer)

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
	} else {
		uploaded, err = inst.WAClient.Upload(context.Background(), data, whatsmeow.MediaDocument)
	}

	if err != nil {
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

	_, err = inst.WAClient.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return fmt.Errorf("erro ao enviar mídia: %w", err)
	}

	return nil
}
