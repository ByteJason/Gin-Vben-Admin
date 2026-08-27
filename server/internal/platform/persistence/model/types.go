// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// JSONValue keeps JSON columns portable: MySQL uses JSON and PostgreSQL uses
// JSONB. It is intentionally a value type so GORM does not infer an
// association from schema-only models.
type JSONValue []byte

func (JSONValue) GormDataType() string { return "json" }
func (JSONValue) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres" {
		return "JSONB"
	}
	return "JSON"
}
func (v JSONValue) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	if !json.Valid(v) {
		return nil, errors.New("invalid JSON value")
	}
	return []byte(v), nil
}
func (v *JSONValue) Scan(value any) error {
	if value == nil {
		*v = nil
		return nil
	}
	switch raw := value.(type) {
	case []byte:
		*v = append((*v)[:0], raw...)
	case string:
		*v = append((*v)[:0], raw...)
	default:
		return fmt.Errorf("scan JSON value from %T", value)
	}
	return nil
}

// BinaryValue maps encrypted blobs to MEDIUMBLOB on MySQL and BYTEA on
// PostgreSQL without putting a dialect-specific type tag on a shared model.
type BinaryValue []byte

func (BinaryValue) GormDataType() string { return "bytes" }
func (BinaryValue) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres" {
		return "BYTEA"
	}
	return "MEDIUMBLOB"
}
func (v BinaryValue) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	return []byte(v), nil
}
func (v *BinaryValue) Scan(value any) error {
	if value == nil {
		*v = nil
		return nil
	}
	switch raw := value.(type) {
	case []byte:
		*v = append((*v)[:0], raw...)
	case string:
		*v = append((*v)[:0], raw...)
	default:
		return fmt.Errorf("scan binary value from %T", value)
	}
	return nil
}

// Compatibility aliases keep existing callers concise.
type JSONData = JSONValue
type BinaryData = BinaryValue
