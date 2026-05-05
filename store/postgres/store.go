package postgres

import (
	"database/sql"
	"fmt"
	"log"
	"wapi/config"
	"wapi/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	DB   *sql.DB
	GORM *gorm.DB
)

func Connect() error {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		config.App.PostgresHost,
		config.App.PostgresPort,
		config.App.PostgresUser,
		config.App.PostgresPassword,
		config.App.PostgresDB,
	)

	// Configuração do GORM
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		return fmt.Errorf("erro ao conectar no postgres via GORM: %w", err)
	}

	GORM = db

	// Extrai o *sql.DB para manter compatibilidade com código legado
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("erro ao extrair sql.DB do GORM: %w", err)
	}

	DB = sqlDB
	return nil
}

func Migrate() error {
	log.Println("Iniciando auto-migration com GORM...")
	
	// A ordem importa para garantir que as FKs sejam criadas corretamente
	err := GORM.AutoMigrate(
		&model.Company{},
		&model.User{},
		&model.Instance{},
		&model.Lead{},
		&model.SystemConfig{},
		&model.SDRAgent{},
		&model.ChatHistory{},
		&model.GlobalConfig{},
		&model.Distributor{},
	)

	if err != nil {
		return fmt.Errorf("erro ao executar auto-migration: %w", err)
	}

	log.Println("Auto-migration finalizada com sucesso!")
	return nil
}
