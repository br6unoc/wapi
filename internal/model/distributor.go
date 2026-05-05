package model

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Distributor struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CompanyID      uuid.UUID  `gorm:"type:uuid;not null;index" json:"company_id"`
	Name           string     `gorm:"not null" json:"name"`
	Phone          string     `gorm:"not null" json:"phone"`
	LeadsCount     int        `gorm:"default:0" json:"leads_count"`
	IsActive       bool       `gorm:"default:true" json:"is_active"`
	LastAssignedAt *time.Time `json:"last_assigned_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	Company Company `gorm:"foreignKey:CompanyID" json:"-"`
}

func (d *Distributor) BeforeCreate(tx *gorm.DB) (err error) {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return
}
