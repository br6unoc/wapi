package whatsapp

import (
	"context"
	"fmt"
	"os"
	"log"

	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func NewClient(jidStr string) (*whatsmeow.Client, *sqlstore.Container, error) {
	// Configura a identidade do dispositivo para o WhatsApp mostrar o nome correto no celular
	store.DeviceProps.Os = proto.String("Orion Engine SDR")
	
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

	var device *store.Device
	
	// Tentar encontrar o dispositivo específico pelo JID (telefone)
	if jid, err := types.ParseJID(jidStr); err == nil {
		device, err = container.GetDevice(context.Background(), jid)
		if err == nil && device != nil {
			log.Printf("[WHATSAPP] Usando dispositivo existente para JID: %s", jid.String())
		}
	}

	// Se não encontrou por JID, cria um novo dispositivo para pareamento
	if device == nil {
		log.Printf("[WHATSAPP] Criando novo dispositivo (sessão limpa) para pareamento")
		device = container.NewDevice()
	}

	client := whatsmeow.NewClient(device, waLog.Noop)
	return client, container, nil
}

func FormatPhone(phone string) types.JID {
	return types.NewJID(phone, types.DefaultUserServer)
}
