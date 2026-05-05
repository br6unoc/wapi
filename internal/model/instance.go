package model

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Instance struct {
	ID                   uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	CompanyID            uuid.UUID `gorm:"type:uuid;not null;index"`
	Name                 string    `gorm:"uniqueIndex;not null"`
	APIKey               string    `gorm:"uniqueIndex;not null"`
	WebhookURL           string    `gorm:"default:''"`
	TranscriptionEnabled bool      `gorm:"default:false"`
	TypingDelayMin      int       `gorm:"default:1000"`
	TypingDelayMax      int       `gorm:"default:3000"`
	Status               string    `gorm:"default:'disconnected'"`
	Phone                string    `gorm:"default:''"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
	
	Company Company `gorm:"foreignKey:CompanyID"`
}

func (i *Instance) BeforeCreate(tx *gorm.DB) (err error) {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}
	return
}
