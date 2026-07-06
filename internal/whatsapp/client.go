package whatsapp

import (
	"context"
	"fmt"
	"log"
	"os"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "github.com/mattn/go-sqlite3"
)

const sessionsDir = "/app/sessions"

type stdLogger struct {
	prefix string
}

func (l *stdLogger) Debugf(msg string, args ...interface{}) {}
func (l *stdLogger) Infof(msg string, args ...interface{}) {
	log.Printf("[WA:"+l.prefix+"] "+msg, args...)
}
func (l *stdLogger) Warnf(msg string, args ...interface{}) {
	log.Printf("[WA:WARN:"+l.prefix+"] "+msg, args...)
}
func (l *stdLogger) Errorf(msg string, args ...interface{}) {
	log.Printf("[WA:ERROR:"+l.prefix+"] "+msg, args...)
}
func (l *stdLogger) Sub(module string) waLog.Logger {
	return &stdLogger{prefix: l.prefix + "/" + module}
}

func init() {
	store.SetOSInfo("BotWapp", [3]uint32{1, 0, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
}

func NewClient(instanceID string) (*whatsmeow.Client, *sqlstore.Container, error) {
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("erro ao criar diretório de sessões: %w", err)
	}

	dbPath := fmt.Sprintf("%s/%s.db", sessionsDir, instanceID)
	logger := &stdLogger{prefix: instanceID[:8]}
	container, err := sqlstore.New(context.Background(), "sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on", dbPath), logger)
	if err != nil {
		return nil, nil, fmt.Errorf("erro ao criar store sqlite: %w", err)
	}

	device, err := container.GetFirstDevice(context.Background())
	if err != nil || device == nil {
		device = container.NewDevice()
	}

	client := whatsmeow.NewClient(device, logger)
	return client, container, nil
}

func FormatPhone(phone string) types.JID {
	return types.NewJID(phone, types.DefaultUserServer)
}
