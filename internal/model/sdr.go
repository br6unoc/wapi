package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// JSONSteps é um helper para armazenar []interface{} como JSONB
type JSONSteps []interface{}

func (j JSONSteps) Value() (driver.Value, error) {
	return json.Marshal(j)
}
func (j *JSONSteps) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("tipo inválido para JSONSteps")
	}
	return json.Unmarshal(bytes, j)
}

type SDRAgent struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CompanyID     uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"company_id"`
	AgentName     string    `gorm:"default:'Clara'" json:"agent_name"`
	CompanyName   string    `json:"company_name"`
	ProviderIA    string    `gorm:"default:'openai'" json:"provider_ia"`
	ModelName     string    `gorm:"default:'gpt-4o-mini'" json:"model_name"`
	ApiKeyIA      string    `json:"api_key_ia"`
	PersonaPrompt string    `json:"persona_prompt"`
	Entropy       float64   `gorm:"default:0.2" json:"entropy"`
	FunnelSteps   JSONSteps `gorm:"type:jsonb" json:"funnel_steps"`
	IsActive      bool      `gorm:"default:true" json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	Company Company `gorm:"foreignKey:CompanyID" json:"-"`
}

func (s *SDRAgent) BeforeCreate(tx *gorm.DB) (err error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return
}
