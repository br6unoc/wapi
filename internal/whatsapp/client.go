package whatsapp

import (
	"context"
	"fmt"
	"os"

	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func NewClient(instanceID string) (*whatsmeow.Client, *sqlstore.Container, error) {
	pgHost := os.Getenv("POSTGRES_HOST")
	pgPort := os.Getenv("POSTGRES_PORT")
	pgUser := os.Getenv("POSTGRES_USER")
	pgPass := os.Getenv("POSTGRES_PASSWORD")
	pgDB := os.Getenv("POSTGRES_DB")

	if pgHost == "" || pgUser == "" || pgPass == "" || pgDB == "" {
		return nil, nil, fmt.Errorf("variáveis de ambiente do Postgres não configuradas")
	}

	if pgPort == "" {
		pgPort = "5432"
	}

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		pgHost, pgPort, pgUser, pgPass, pgDB)

	container, err := sqlstore.New(context.Background(), "postgres", dsn, waLog.Noop)
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao criar store postgres: %w", err)
	}

	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao obter device: %w", err)
	}

	client := whatsmeow.NewClient(device, waLog.Noop)
	return client, container, nil
}

func FormatPhone(phone string) types.JID {
	return types.NewJID(phone, types.DefaultUserServer)
}
