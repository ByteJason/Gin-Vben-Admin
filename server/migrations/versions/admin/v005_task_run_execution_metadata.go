package admin

import (
	"errors"
	"fmt"

	persistencemodel "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
)

// V005Version adds execution provenance, timing, result and redacted output fields.
const V005Version = "v005_task_run_execution_metadata"

var taskRunMetadataColumns = []struct {
	model   any
	columns []string
}{
	{&persistencemodel.TaskDefinition{}, []string{"executor_type", "method_key", "http_config", "description", "payload"}},
	{&persistencemodel.TaskRun{}, []string{"task_name", "task_description", "config_snapshot", "error_code", "trigger_source", "executor_type", "duration_ms", "result_summary", "redacted_output"}},
	{&persistencemodel.TaskRunLog{}, []string{"trigger_source", "executor_type", "started_at", "finished_at", "duration_ms", "result_summary", "redacted_output"}},
}

func UpV005(db *gorm.DB) error {
	if db == nil || db.Dialector == nil {
		return errors.New("task execution metadata migration database is not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, item := range taskRunMetadataColumns {
			if !tx.Migrator().HasTable(item.model) {
				continue
			}
			for _, column := range item.columns {
				if tx.Migrator().HasColumn(item.model, column) {
					continue
				}
				if err := tx.Migrator().AddColumn(item.model, column); err != nil {
					return fmt.Errorf("add task metadata column %s: %w", column, err)
				}
			}
		}
		if tx.Migrator().HasTable(&persistencemodel.TaskDefinition{}) {
			if err := tx.Model(&persistencemodel.TaskDefinition{}).Where("type IN ? AND executor_type <> ?", []string{"http", "webhook"}, "http").Update("executor_type", "http").Error; err != nil {
				return err
			}
		}
		return moveTaskMenu(tx, "menu-system-config", "/system/tasks", "lucide:clock-3")
	})
}

func DownV005(db *gorm.DB) error {
	if db == nil || db.Dialector == nil {
		return errors.New("task execution metadata migration database is not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for i := len(taskRunMetadataColumns) - 1; i >= 0; i-- {
			item := taskRunMetadataColumns[i]
			if !tx.Migrator().HasTable(item.model) {
				continue
			}
			for j := len(item.columns) - 1; j >= 0; j-- {
				column := item.columns[j]
				if tx.Migrator().HasColumn(item.model, column) {
					if err := tx.Migrator().DropColumn(item.model, column); err != nil {
						return fmt.Errorf("drop task metadata column %s: %w", column, err)
					}
				}
			}
		}
		return moveTaskMenu(tx, "menu-operations", "/ops/tasks", "lucide:workflow")
	})
}

func moveTaskMenu(db *gorm.DB, parent, path, icon string) error {
	if !db.Migrator().HasTable(&persistencemodel.Menu{}) {
		return nil
	}
	return db.Model(&persistencemodel.Menu{}).Where("id = ? AND path IN ?", "menu-operations-tasks", []string{"/ops/tasks", "/system/tasks"}).Updates(map[string]any{"parent_id": parent, "path": path, "icon": icon}).Error
}
