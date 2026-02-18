package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
	"wapi/config"
	"wapi/store/postgres"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func Login(username, password string) (string, error) {
	var id, hashedPassword string

	err := postgres.DB.QueryRow(
		"SELECT id, password FROM users WHERE username = $1",
		username,
	).Scan(&id, &hashedPassword)

	if err == sql.ErrNoRows {
		return "", errors.New("usuário não encontrado")
	}
	if err != nil {
		return "", fmt.Errorf("erro ao buscar usuário: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)); err != nil {
		return "", errors.New("senha incorreta")
	}

	return generateToken(id, username)
}

func generateToken(userID, username string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.App.JWTSecret))
}

func ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(config.App.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("token inválido")
	}

	return claims, nil
}

func CreateAdminIfNotExists(username, password string) error {
	var count int
	err := postgres.DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = $1", username).Scan(&count)
	if err != nil {
		return fmt.Errorf("erro ao verificar usuário: %w", err)
	}

	if count > 0 {
		return nil
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("erro ao gerar hash: %w", err)
	}

	id := uuid.New().String()
	_, err = postgres.DB.Exec(
		"INSERT INTO users (id, username, password) VALUES ($1, $2, $3)",
		id, username, string(hashed),
	)
	if err != nil {
		return fmt.Errorf("erro ao criar admin: %w", err)
	}

	return nil
}
