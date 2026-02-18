package whatsapp

import (
	"context"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func NewClient(instanceID string) (*whatsmeow.Client, error) {
	dbPath := fmt.Sprintf("./sessions/%s.db?_foreign_keys=on", instanceID)

	if err := os.MkdirAll("./sessions", 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar pasta sessions: %w", err)
	}

	container, err := sqlstore.New(context.Background(), "sqlite3", dbPath, waLog.Noop)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar store: %w", err)
	}

	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("erro ao obter device: %w", err)
	}

	client := whatsmeow.NewClient(device, waLog.Noop)
	return client, nil
}

func FormatPhone(phone string) types.JID {
	return types.NewJID(phone, types.DefaultUserServer)
}
