package model

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Lead struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CompanyID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"company_id"`
	InstanceID      *uuid.UUID `gorm:"type:uuid;index" json:"instance_id"`
	Name            string     `json:"name"`
	Phone           string     `gorm:"not null" json:"phone"`
	Status          string     `gorm:"default:'PENDING'" json:"status"`
	IsQualified     bool       `gorm:"default:false" json:"is_qualified"`
	InvestmentValue string     `json:"investment_value"`
	Location        string     `json:"location"`
	QualifiedAt     *time.Time `json:"qualified_at"`
	ListName        string     `json:"list_name"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	Company Company `gorm:"foreignKey:CompanyID" json:"-"`
}

func (l *Lead) BeforeCreate(tx *gorm.DB) (err error) {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return
}
