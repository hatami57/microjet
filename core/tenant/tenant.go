package tenant

import (
	"time"

	"github.com/google/uuid"
)

type Tenant interface {
	AsBase() *Base
}

type Base struct {
	ID        uuid.UUID `json:"id" gorm:"primaryKey;type:uuid"`
	Code      string    `json:"code" gorm:"type:varchar(50);uniqueIndex;not null"`
	IsActive  bool      `json:"isActive" gorm:"not null"`
	CreatedAt time.Time `json:"createdAt" gorm:"type:timestamp;not null"`
	UpdatedAt time.Time `json:"updatedAt" gorm:"type:timestamp;not null"`
}

func (b *Base) AsBase() *Base { return b }
