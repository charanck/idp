package model

import (
	"context"
	"time"
)

// Branding is a singleton (id=1) settings row for login-screen/product
// branding, kept separate from Policy since it's a cosmetic concern, not an
// access-control one. Empty ProductName/LogoURL mean "use the built-in
// default" (product name "Control Plane", no logo image). Empty AccentColor
// means "use the built-in default accent"; empty BackgroundImageURL means
// "no background image" (plain background).
type Branding struct {
	ID                 int16     `gorm:"column:id;primaryKey"`
	ProductName        string    `gorm:"column:product_name"`
	LogoURL            string    `gorm:"column:logo_url"`
	AccentColor        string    `gorm:"column:accent_color"`
	BackgroundImageURL string    `gorm:"column:background_image_url"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Branding) TableName() string { return "branding" }

// BrandingRepository is the persistence boundary for the singleton Branding row.
type BrandingRepository interface {
	Get(ctx context.Context) (*Branding, error)
	Update(ctx context.Context, branding *Branding) error
}
