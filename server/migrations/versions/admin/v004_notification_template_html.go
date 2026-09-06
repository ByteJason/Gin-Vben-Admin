package admin

import (
	"errors"

	persistencemodel "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
)

// V004Version adds the persisted body format for template locales, snapshots,
// and durable email messages. Existing rows default to text/plain.
const V004Version = "v004_notification_template_html"

func UpV004(db *gorm.DB) error {
	if db == nil || db.Dialector == nil {
		return errors.New("notification template html migration database is not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{&persistencemodel.NotificationTemplateLocale{}, &persistencemodel.NotificationTemplateVersion{}, &persistencemodel.EmailMessage{}} {
			if !tx.Migrator().HasTable(model) || tx.Migrator().HasColumn(model, "body_format") {
				continue
			}
			if err := tx.Migrator().AddColumn(model, "BodyFormat"); err != nil {
				return err
			}
		}
		return nil
	})
}

func DownV004(db *gorm.DB) error {
	if db == nil || db.Dialector == nil {
		return errors.New("notification template html migration database is not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{&persistencemodel.NotificationTemplateLocale{}, &persistencemodel.NotificationTemplateVersion{}, &persistencemodel.EmailMessage{}} {
			if tx.Migrator().HasTable(model) && tx.Migrator().HasColumn(model, "body_format") {
				if err := tx.Migrator().DropColumn(model, "BodyFormat"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
