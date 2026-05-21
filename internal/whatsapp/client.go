package whatsapp

import (
	"context"
	"fmt"
	"os"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "github.com/mattn/go-sqlite3"
)

const sessionsDir = "/app/sessions"

func NewClient(instanceID string) (*whatsmeow.Client, *sqlstore.Container, error) {
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("erro ao criar diretório de sessões: %w", err)
	}

	dbPath := fmt.Sprintf("%s/%s.db", sessionsDir, instanceID)
	container, err := sqlstore.New(context.Background(), "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), waLog.Noop)
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao criar store sqlite: %w", err)
	}

	device, err := container.GetFirstDevice(context.Background())
	if err != nil || device == nil {
		device = container.NewDevice()
	}

	client := whatsmeow.NewClient(device, waLog.Noop)
	return client, container, nil
}

func FormatPhone(phone string) types.JID {
	return types.NewJID(phone, types.DefaultUserServer)
}
