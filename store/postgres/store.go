package postgres

import (
	"database/sql"
	"fmt"
	"wapi/config"

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
	`

	_, err := DB.Exec(query)
	if err != nil {
		return fmt.Errorf("erro ao executar migration: %w", err)
	}

	return nil
}
