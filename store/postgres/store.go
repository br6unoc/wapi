package postgres

import (
	"database/sql"
	"fmt"
	"log"
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
	CREATE TABLE IF NOT EXISTS companies (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name VARCHAR(255) NOT NULL,
		max_users INTEGER NOT NULL DEFAULT 5,
		max_instances INTEGER NOT NULL DEFAULT 3,
		active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMP DEFAULT NOW(),
		updated_at TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		username VARCHAR(255) UNIQUE NOT NULL,
		password VARCHAR(255) NOT NULL,
		created_at TIMESTAMP DEFAULT NOW()
	);

	ALTER TABLE users ADD COLUMN IF NOT EXISTS role VARCHAR(20) NOT NULL DEFAULT 'admin';
	ALTER TABLE users ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES companies(id) ON DELETE CASCADE;

	CREATE TABLE IF NOT EXISTS instances (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		name VARCHAR(255) NOT NULL,
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

	ALTER TABLE instances ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES companies(id) ON DELETE CASCADE;

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
	CREATE INDEX IF NOT EXISTS idx_users_company ON users (company_id);
	CREATE INDEX IF NOT EXISTS idx_instances_company ON instances (company_id);

	ALTER TABLE contacts ADD COLUMN IF NOT EXISTS unread_count INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_path TEXT NOT NULL DEFAULT '';
	ALTER TABLE messages ADD COLUMN IF NOT EXISTS media_name TEXT NOT NULL DEFAULT '';

	CREATE TABLE IF NOT EXISTS settings (
		key VARCHAR(255) PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);
	`

	if _, err := DB.Exec(query); err != nil {
		return fmt.Errorf("erro ao executar migration: %w", err)
	}

	// Troca UNIQUE(name) por UNIQUE(company_id, name) nas instâncias
	DB.Exec(`ALTER TABLE instances DROP CONSTRAINT IF EXISTS instances_name_key`)
	DB.Exec(`
		DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'instances_company_id_name_key'
			) THEN
				ALTER TABLE instances ADD CONSTRAINT instances_company_id_name_key UNIQUE (company_id, name);
			END IF;
		END $$;
	`)

	DB.Exec(`ALTER TABLE companies ADD COLUMN IF NOT EXISTS plan_value DECIMAL(10,2) NOT NULL DEFAULT 0`)

	DB.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
			instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			prompt TEXT NOT NULL,
			contact_type VARCHAR(20) NOT NULL CHECK (contact_type IN ('first_contact', 'returning')),
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			handoff_keyword VARCHAR(100) NOT NULL DEFAULT 'PRECISO_DE_HUMANO',
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE (instance_id, contact_type)
		)
	`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS agent_mode BOOLEAN NOT NULL DEFAULT TRUE`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS is_first_contact BOOLEAN NOT NULL DEFAULT TRUE`)

	// Setores e atribuições de usuários
	DB.Exec(`
		CREATE TABLE IF NOT EXISTS sectors (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(company_id, name)
		)
	`)
	DB.Exec(`
		CREATE TABLE IF NOT EXISTS user_sectors (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			sector_id UUID NOT NULL REFERENCES sectors(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, sector_id)
		)
	`)
	DB.Exec(`
		CREATE TABLE IF NOT EXISTS user_instances (
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, instance_id)
		)
	`)

	if err := migrateToMultiTenant(); err != nil {
		log.Printf("[MIGRATE] Aviso na migração multi-tenant: %v", err)
	}

	return nil
}

// migrateToMultiTenant cria a empresa principal do super_admin se ainda não existir,
// promove o admin configurado para super_admin e vincula instâncias órfãs.
func migrateToMultiTenant() error {
	// Verifica se já existe super_admin — se sim, migração já foi feita
	var count int
	DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'super_admin'`).Scan(&count)
	if count > 0 {
		return nil
	}

	// Cria a empresa "Principal" para o super_admin
	var companyID string
	err := DB.QueryRow(`
		INSERT INTO companies (name, max_users, max_instances)
		VALUES ('Principal', 999, 999)
		RETURNING id
	`).Scan(&companyID)
	if err != nil {
		return fmt.Errorf("criar empresa principal: %w", err)
	}

	// Promove o admin configurado para super_admin e vincula à empresa
	_, err = DB.Exec(`
		UPDATE users
		SET role = 'super_admin', company_id = $1
		WHERE username = $2
	`, companyID, config.App.AdminUser)
	if err != nil {
		return fmt.Errorf("promover super_admin: %w", err)
	}

	// Vincula instâncias sem empresa à empresa principal
	_, err = DB.Exec(`UPDATE instances SET company_id = $1 WHERE company_id IS NULL`, companyID)
	if err != nil {
		return fmt.Errorf("vincular instâncias: %w", err)
	}

	log.Printf("[MIGRATE] Super_admin '%s' promovido. Empresa 'Principal' criada (id=%s)", config.App.AdminUser, companyID)
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

func GetCompanySetting(companyID, key string) (string, error) {
	return GetSetting(companyID + ":" + key)
}

func SetCompanySetting(companyID, key, value string) error {
	return SetSetting(companyID+":"+key, value)
}
