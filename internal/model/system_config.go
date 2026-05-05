package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// JSONMap é um helper para armazenar map[string]interface{} como JSONB
type JSONMap map[string]interface{}

func (j JSONMap) Value() (driver.Value, error) {
	return json.Marshal(j)
}
func (j *JSONMap) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("tipo inválido para JSONMap")
	}
	return json.Unmarshal(bytes, j)
}

// JSONStrings é um helper para armazenar []string como JSONB
type JSONStrings []string

func (j JSONStrings) Value() (driver.Value, error) {
	return json.Marshal(j)
}
func (j *JSONStrings) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("tipo inválido para JSONStrings")
	}
	return json.Unmarshal(bytes, j)
}

type SystemConfig struct {
	ID          uuid.UUID   `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CompanyID   uuid.UUID   `gorm:"type:uuid;uniqueIndex;not null"`
	IsActive    bool        `gorm:"default:false"`
	WindowStart string      `gorm:"default:'08:00'"`
	WindowEnd   string      `gorm:"default:'18:00'"`
	ActiveDays  JSONMap     `gorm:"type:jsonb"`
	IntervalMin int         `gorm:"default:180"`
	IntervalMax int         `gorm:"default:360"`
	WebhookURL  string
	RetryLimit  int         `gorm:"default:3"`
	APIURL      string
	APIToken    string
	Messages    JSONStrings `gorm:"type:jsonb"`
	CreatedAt   time.Time
	UpdatedAt   time.Time

	Company Company `gorm:"foreignKey:CompanyID"`
}

func (s *SystemConfig) BeforeCreate(tx *gorm.DB) (err error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return
}
