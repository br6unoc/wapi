package model

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Company struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Name          string    `gorm:"not null" json:"name"`
	AdminEmail    string    `gorm:"uniqueIndex;not null" json:"admin_email"`
	Status        string    `gorm:"default:'Ativo'" json:"status"`
	UserLimit     int       `gorm:"default:5" json:"user_limit"`
	WhatsappLimit int       `gorm:"default:1" json:"whatsapp_limit"`
	AiAgentEnabled bool     `gorm:"default:true" json:"ai_agent_enabled"`
	ExpiryDate    time.Time `gorm:"not null" json:"expiry_date"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (c *Company) BeforeCreate(tx *gorm.DB) (err error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return
}
