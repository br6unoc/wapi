package model

import (
	"time"
	"github.com/google/uuid"
)

type ChatHistory struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CompanyID    uuid.UUID `gorm:"type:uuid;index" json:"company_id"`
	SenderNumber string    `gorm:"index" json:"sender_number"`
	Role         string    `json:"role"` // "user" ou "assistant"
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
}
