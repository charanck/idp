package main

import (
	"errors"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"controlplane/internal/appconfig"
	authmodel "controlplane/internal/model/auth"
	"controlplane/internal/security"
)

// bootstrapAdmin mirrors authentication/management/commands/setup_admin.py:
// on every startup, ADMIN_EMAIL/ADMIN_PASSWORD (if set) create the admin user
// if missing, or sync an existing user's superuser/staff/active flags and
// password. A missing ADMIN_EMAIL is a no-op, not an error, matching the
// management command's behavior when run with no email available.
func bootstrapAdmin(gdb *gorm.DB, cfg *appconfig.Config) error {
	if cfg.AdminEmail == "" {
		return nil
	}
	password := cfg.AdminPassword
	if password == "" {
		password = "admin123"
	}

	var user authmodel.User
	err := gdb.Where("email = ?", cfg.AdminEmail).First(&user).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		hashed, hashErr := security.HashPassword(password)
		if hashErr != nil {
			return hashErr
		}
		newUser := authmodel.User{
			Email:              cfg.AdminEmail,
			Username:           strings.SplitN(cfg.AdminEmail, "@", 2)[0],
			Password:           hashed,
			IsSuperuser:        true,
			IsStaff:            true,
			IsActive:           true,
			ForcePasswordReset: true,
		}
		if createErr := gdb.Create(&newUser).Error; createErr != nil {
			return createErr
		}
		slog.Info("created admin user", "email", cfg.AdminEmail)
		return nil
	case err != nil:
		return err
	default:
		hashed, hashErr := security.HashPassword(password)
		if hashErr != nil {
			return hashErr
		}
		user.IsSuperuser = true
		user.IsStaff = true
		user.IsActive = true
		user.Password = hashed
		user.ForcePasswordReset = true
		if saveErr := gdb.Save(&user).Error; saveErr != nil {
			return saveErr
		}
		slog.Info("synced admin user credentials", "email", cfg.AdminEmail)
		return nil
	}
}
