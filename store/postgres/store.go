package postgres

import (
	"database/sql"
	"fmt"
	"log"
	"time"
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

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

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
		company_id UUID REFERENCES companies(id) ON DELETE CASCADE,
		created_at TIMESTAMP DEFAULT NOW()
	);
	ALTER TABLE api_tokens ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES companies(id) ON DELETE CASCADE;

	CREATE TABLE IF NOT EXISTS contacts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		instance_id UUID REFERENCES instances(id) ON DELETE SET NULL,
		phone VARCHAR(50) NOT NULL,
		name VARCHAR(255) DEFAULT '',
		created_at TIMESTAMP DEFAULT NOW(),
		UNIQUE(instance_id, phone)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		instance_id UUID REFERENCES instances(id) ON DELETE SET NULL,
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

	if _, err := DB.Exec(`CREATE TABLE IF NOT EXISTS agents (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE, instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE, name VARCHAR(255) NOT NULL, prompt TEXT NOT NULL, contact_type VARCHAR(20) NOT NULL CHECK (contact_type IN ('first_contact', 'returning')), is_active BOOLEAN NOT NULL DEFAULT TRUE, handoff_keyword VARCHAR(100) NOT NULL DEFAULT 'PRECISO_DE_HUMANO', created_at TIMESTAMP DEFAULT NOW(), UNIQUE (instance_id, contact_type))`); err != nil {
		log.Printf("[MIGRATE] Aviso na criação da tabela agents: %v", err)
	}

	if _, err := DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS agent_mode BOOLEAN NOT NULL DEFAULT TRUE`); err != nil {
		log.Printf("[MIGRATE] Aviso ao adicionar agent_mode: %v", err)
	}

	if _, err := DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS is_first_contact BOOLEAN NOT NULL DEFAULT TRUE`); err != nil {
		log.Printf("[MIGRATE] Aviso ao adicionar is_first_contact: %v", err)
	}

	// Refactor: agentes são templates reutilizáveis; instâncias têm FKs para agentes
	DB.Exec(`ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_instance_id_contact_type_key`)
	DB.Exec(`ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_contact_type_check`)
	DB.Exec(`ALTER TABLE agents DROP COLUMN IF EXISTS instance_id`)
	DB.Exec(`ALTER TABLE agents DROP COLUMN IF EXISTS contact_type`)
	DB.Exec(`ALTER TABLE instances ADD COLUMN IF NOT EXISTS first_contact_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL`)
	DB.Exec(`ALTER TABLE instances ADD COLUMN IF NOT EXISTS returning_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL`)

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

	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS conv_status VARCHAR(20) NOT NULL DEFAULT 'open'`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS assigned_user_id UUID REFERENCES users(id) ON DELETE SET NULL`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_contacts_conv_status ON contacts (conv_status)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_contacts_assigned ON contacts (assigned_user_id)`)

	DB.Exec(`
		CREATE TABLE IF NOT EXISTS instance_sectors (
			instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
			sector_id UUID NOT NULL REFERENCES sectors(id) ON DELETE CASCADE,
			PRIMARY KEY (instance_id, sector_id)
		)
	`)

	DB.Exec(`
		CREATE TABLE IF NOT EXISTS contact_tags (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL,
			color VARCHAR(7) NOT NULL DEFAULT '#6366f1',
			created_at TIMESTAMP DEFAULT NOW(),
			UNIQUE(company_id, name)
		)
	`)
	DB.Exec(`
		CREATE TABLE IF NOT EXISTS contact_tag_links (
			contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
			tag_id UUID NOT NULL REFERENCES contact_tags(id) ON DELETE CASCADE,
			PRIMARY KEY (contact_id, tag_id)
		)
	`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS is_blocked BOOLEAN NOT NULL DEFAULT FALSE`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS last_contact_at TIMESTAMP`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS purchase_count INTEGER NOT NULL DEFAULT 0`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_contacts_last_contact ON contacts (last_contact_at)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_contacts_instance_created ON contacts (instance_id, created_at)`)

	DB.Exec(`
		CREATE TABLE IF NOT EXISTS campaigns (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
			created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name VARCHAR(255) NOT NULL,
			message TEXT NOT NULL,
			instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
			audience_type VARCHAR(20) NOT NULL DEFAULT 'all',
			audience_ref UUID,
			schedule_type VARCHAR(20) NOT NULL DEFAULT 'now',
			scheduled_at TIMESTAMP,
			status VARCHAR(20) NOT NULL DEFAULT 'draft',
			total_contacts INTEGER NOT NULL DEFAULT 0,
			sent_count INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT NOW(),
			started_at TIMESTAMP,
			finished_at TIMESTAMP
		)
	`)
	DB.Exec(`
		CREATE TABLE IF NOT EXISTS campaign_contacts (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
			phone VARCHAR(50) NOT NULL,
			name VARCHAR(255) NOT NULL DEFAULT '',
			status VARCHAR(20) NOT NULL DEFAULT 'pending',
			sent_at TIMESTAMP,
			error_msg TEXT NOT NULL DEFAULT '',
			UNIQUE(campaign_id, phone)
		)
	`)
	DB.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS filters JSONB NOT NULL DEFAULT '{}'`)
	DB.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS drip_min_seconds INT NOT NULL DEFAULT 30`)
	DB.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS drip_max_seconds INT NOT NULL DEFAULT 60`)
	DB.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS message_variants JSONB NOT NULL DEFAULT '[]'`)
	DB.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS send_start_time VARCHAR(5) NOT NULL DEFAULT ''`)
	DB.Exec(`ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS send_end_time VARCHAR(5) NOT NULL DEFAULT ''`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_campaigns_company ON campaigns (company_id, created_at DESC)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_campaign_contacts_status ON campaign_contacts (campaign_id, status)`)
	DB.Exec(`ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_edited BOOLEAN NOT NULL DEFAULT FALSE`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_wa_id ON messages (wa_message_id) WHERE wa_message_id != ''`)

	DB.Exec(`CREATE TABLE IF NOT EXISTS products (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
		name VARCHAR(255) NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		price DECIMAL(10,2),
		active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMP DEFAULT NOW()
	)`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_products_company ON products (company_id)`)

	DB.Exec(`ALTER TABLE agents ADD COLUMN IF NOT EXISTS followup_enabled BOOLEAN NOT NULL DEFAULT FALSE`)
	DB.Exec(`ALTER TABLE agents ADD COLUMN IF NOT EXISTS followup_intervals JSONB NOT NULL DEFAULT '[120, 1440, 4320]'`)
	DB.Exec(`ALTER TABLE agents ADD COLUMN IF NOT EXISTS followup_max INTEGER NOT NULL DEFAULT 3`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS followup_count INTEGER NOT NULL DEFAULT 0`)
	DB.Exec(`ALTER TABLE contacts ADD COLUMN IF NOT EXISTS last_followup_at TIMESTAMP`)

	DB.Exec(`
		CREATE TABLE IF NOT EXISTS wa_sync_jids (
			instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
			jid TEXT NOT NULL,
			phone TEXT NOT NULL DEFAULT '',
			wa_name TEXT NOT NULL DEFAULT '',
			last_msg_at TIMESTAMP,
			synced_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (instance_id, jid)
		)
	`)
	DB.Exec(`CREATE INDEX IF NOT EXISTS idx_wa_sync_jids_phone ON wa_sync_jids (instance_id, phone)`)

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

func GetMessageMediaPath(msgID, companyID string) (string, error) {
	var path string
	err := DB.QueryRow(`
		SELECT m.media_path FROM messages m
		JOIN contacts ct ON ct.id = m.contact_id
		JOIN instances i ON i.id = ct.instance_id
		WHERE m.id = $1 AND i.company_id = $2
	`, msgID, companyID).Scan(&path)
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

type ContactBasic struct {
	ID           string
	Phone        string
	InstanceName string
}

func GetContactBasic(contactID string) (ContactBasic, error) {
	var c ContactBasic
	err := DB.QueryRow(`
		SELECT ct.id, ct.phone, i.name
		FROM contacts ct JOIN instances i ON i.id = ct.instance_id
		WHERE ct.id = $1
	`, contactID).Scan(&c.ID, &c.Phone, &c.InstanceName)
	return c, err
}

func SetContactBlocked(contactID string, blocked bool) error {
	_, err := DB.Exec(`UPDATE contacts SET is_blocked = $1 WHERE id = $2`, blocked, contactID)
	return err
}

func DeleteContact(contactID string) error {
	_, err := DB.Exec(`DELETE FROM contacts WHERE id = $1`, contactID)
	return err
}

func IncrementContactPurchase(contactID string) error {
	_, err := DB.Exec(`UPDATE contacts SET purchase_count = purchase_count + 1 WHERE id = $1`, contactID)
	return err
}

func DecrementContactPurchase(contactID string) error {
	_, err := DB.Exec(`UPDATE contacts SET purchase_count = GREATEST(0, purchase_count - 1) WHERE id = $1`, contactID)
	return err
}

func GetCompanySetting(companyID, key string) (string, error) {
	return GetSetting(companyID + ":" + key)
}

func SetCompanySetting(companyID, key, value string) error {
	return SetSetting(companyID+":"+key, value)
}

func ClearWASyncJIDs(instanceID string) error {
	_, err := DB.Exec(`DELETE FROM wa_sync_jids WHERE instance_id = $1`, instanceID)
	return err
}

func UpsertWASyncJID(instanceID, jid, phone, waName string, lastMsgAt *time.Time) error {
	_, err := DB.Exec(`
		INSERT INTO wa_sync_jids (instance_id, jid, phone, wa_name, last_msg_at, synced_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (instance_id, jid) DO UPDATE
		SET phone = EXCLUDED.phone, wa_name = EXCLUDED.wa_name,
		    last_msg_at = EXCLUDED.last_msg_at, synced_at = NOW()
	`, instanceID, jid, phone, waName, lastMsgAt)
	return err
}

type WASyncJID struct {
	Phone        string
	WAName       string
	LastMsgAt    *time.Time
	BotWappName  string
	BotWappPhone string
	InWA         bool
	InBotWapp    bool
}

func GetWASyncCrossRef(instanceID string) ([]WASyncJID, error) {
	rows, err := DB.Query(`
		SELECT
			COALESCE(j.phone, ct.phone)             AS numero,
			COALESCE(j.wa_name, '')                 AS wa_name,
			j.last_msg_at,
			COALESCE(ct.name, '')                   AS botwapp_name,
			COALESCE(ct.phone, '')                  AS botwapp_phone,
			j.phone IS NOT NULL                     AS in_wa,
			ct.phone IS NOT NULL                    AS in_botwapp
		FROM wa_sync_jids j
		FULL OUTER JOIN contacts ct
			ON ct.phone = j.phone AND ct.instance_id = j.instance_id
		WHERE (j.instance_id = $1 OR ct.instance_id = $1)
		  AND (j.jid IS NULL OR j.jid NOT LIKE '%@g.us')
		ORDER BY j.last_msg_at DESC NULLS LAST
	`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []WASyncJID
	for rows.Next() {
		var r WASyncJID
		if err := rows.Scan(&r.Phone, &r.WAName, &r.LastMsgAt, &r.BotWappName, &r.BotWappPhone, &r.InWA, &r.InBotWapp); err != nil {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

