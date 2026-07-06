package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port             string
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDB       string
	RedisHost        string
	RedisPort        string
	JWTSecret        string
	WhisperURL       string
	AdminUser        string
	AdminPassword    string
}

var App Config

func Load() {
	if err := godotenv.Load(); err != nil {
		log.Println("Arquivo .env não encontrado, usando variáveis de ambiente do sistema")
	}

	App = Config{
		Port:             getEnv("PORT", "8080"),
		PostgresHost:     getEnv("POSTGRES_HOST", "localhost"),
		PostgresPort:     getEnv("POSTGRES_PORT", "5432"),
		PostgresUser:     getEnv("POSTGRES_USER", "wapi"),
		PostgresPassword: getEnv("POSTGRES_PASSWORD", "wapi123"),
		PostgresDB:       getEnv("POSTGRES_DB", "wapi"),
		RedisHost:        getEnv("REDIS_HOST", "localhost"),
		RedisPort:        getEnv("REDIS_PORT", "6379"),
		JWTSecret:        getEnv("JWT_SECRET", "chave-secreta-padrao"),
		WhisperURL:       getEnv("WHISPER_URL", "http://localhost:9000"),
		AdminUser:        getEnv("ADMIN_USER", "admin"),
		AdminPassword:    getEnv("ADMIN_PASSWORD", "admin123"),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
