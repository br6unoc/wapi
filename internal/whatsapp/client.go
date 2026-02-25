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

func NewClient(instanceID string) (*whatsmeow.Client, error) {
	// Construir DSN do Postgres a partir das variáveis de ambiente
	pgHost := os.Getenv("POSTGRES_HOST")
	pgPort := os.Getenv("POSTGRES_PORT")
	pgUser := os.Getenv("POSTGRES_USER")
	pgPass := os.Getenv("POSTGRES_PASSWORD")
	pgDB := os.Getenv("POSTGRES_DB")

	if pgHost == "" || pgUser == "" || pgPass == "" || pgDB == "" {
		return nil, fmt.Errorf("variáveis de ambiente do Postgres não configuradas")
	}

	if pgPort == "" {
		pgPort = "5432"
	}

	// DSN do Postgres
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		pgHost, pgPort, pgUser, pgPass, pgDB)

	// Criar store do whatsmeow com Postgres
	container, err := sqlstore.New(context.Background(), "postgres", dsn, waLog.Noop)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar store postgres: %w", err)
	}

	// Obter ou criar device para esta instância
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
