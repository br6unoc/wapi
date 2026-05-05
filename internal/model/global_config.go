package model

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GlobalConfig struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	IAProvider    string    `gorm:"default:'openai'" json:"ia_provider"`
	IAModel       string    `gorm:"default:'gpt-4o-mini'" json:"ia_model"`
	IAKey         string    `json:"ia_key"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (g *GlobalConfig) BeforeCreate(tx *gorm.DB) (err error) {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	return
}
