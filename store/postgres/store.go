package postgres

import (
	"database/sql"
	"fmt"
	"botwapp/config"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func Connect() error {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.App.PostgresHost,
		config.App.PostgresPort,
		config.App.PostgresUser,
		config.App.PostgresPassword,
		config.App.PostgresDB,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("erro ao abrir conexão: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("erro ao conectar no postgres: %w", err)
	}

	DB = db
	return nil
}

func Migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		username VARCHAR(255) UNIQUE NOT NULL,
		password VARCHAR(255) NOT NULL,
		created_at TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS instances (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name VARCHAR(255) UNIQUE NOT NULL,
		api_key VARCHAR(255) UNIQUE NOT NULL,
		webhook_url TEXT DEFAULT '',
		transcription_enabled BOOLEAN DEFAULT FALSE,
		typing_delay_min INTEGER DEFAULT 1000,
		typing_delay_max INTEGER DEFAULT 3000,
		status VARCHAR(50) DEFAULT 'disconnected',
		phone VARCHAR(50) DEFAULT '',
		created_at TIMESTAMP DEFAULT NOW(),
		updated_at TIMESTAMP DEFAULT NOW()
	);
        CREATE TABLE IF NOT EXISTS api_tokens (
                id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                name VARCHAR(255) NOT NULL,
                token TEXT NOT NULL,
                created_at TIMESTAMP DEFAULT NOW()
        );

	CREATE TABLE IF NOT EXISTS contacts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
		phone VARCHAR(50) NOT NULL,
		name VARCHAR(255) DEFAULT '',
		created_at TIMESTAMP DEFAULT NOW(),
		UNIQUE(instance_id, phone)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
		contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		direction VARCHAR(3) NOT NULL CHECK (direction IN ('in', 'out')),
		content TEXT NOT NULL DEFAULT '',
		type VARCHAR(20) NOT NULL DEFAULT 'text',
		wa_message_id VARCHAR(255) DEFAULT '',
		created_at TIMESTAMP DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages (contact_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_instance ON messages (instance_id, created_at DESC);

	ALTER TABLE contacts ADD COLUMN IF NOT EXISTS unread_count INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_path TEXT NOT NULL DEFAULT '';
	ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_name TEXT NOT NULL DEFAULT '';

	CREATE TABLE IF NOT EXISTS settings (
		key VARCHAR(255) PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);
	`

	_, err := DB.Exec(query)
	if err != nil {
		return fmt.Errorf("erro ao executar migration: %w", err)
	}

	// Corrige colunas residuais de versões anteriores que possam bloquear INSERTs
	DB.Exec(`ALTER TABLE instances ALTER COLUMN company_id DROP NOT NULL`)
	DB.Exec(`ALTER TABLE users ALTER COLUMN company_id DROP NOT NULL`)

	return nil
}

func GetMessageMediaPath(msgID string) (string, error) {
	var path string
	err := DB.QueryRow(`SELECT media_path FROM messages WHERE id = $1`, msgID).Scan(&path)
	if err != nil {
		return "", err
	}
	return path, nil
}

func UpdateMessageContent(msgID, content string) error {
	_, err := DB.Exec(`UPDATE messages SET content = $1 WHERE id = $2`, content, msgID)
	return err
}

func GetSetting(key string) (string, error) {
	var value string
	err := DB.QueryRow(`SELECT value FROM settings WHERE key = $1`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func SetSetting(key, value string) error {
	_, err := DB.Exec(`
		INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
	`, key, value)
	return err
}
