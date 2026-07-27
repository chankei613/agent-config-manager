package db

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Init(path string) (*gorm.DB, error) {
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := conn.AutoMigrate(
		&ConfigRoot{},
		&ConfigFile{},
		&Snapshot{},
		&SnapshotEntry{},
		&Template{},
		&TemplateEntry{},
	); err != nil {
		return nil, err
	}

	return conn, nil
}
