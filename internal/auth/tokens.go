package auth

import (
	"fmt"
	"time"
	"wapi/store/postgres"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"wapi/config"
)

type APIToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
}

func CreatePermanentToken(name string) (*APIToken, error) {
	claims := Claims{
		UserID:   "api",
		Username: name,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(config.App.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("erro ao gerar token: %w", err)
	}

	id := uuid.New().String()
	_, err = postgres.DB.Exec(
		"INSERT INTO api_tokens (id, name, token) VALUES ($1, $2, $3)",
		id, name, tokenStr,
	)
	if err != nil {
		return nil, fmt.Errorf("erro ao salvar token: %w", err)
	}

	return &APIToken{ID: id, Name: name, Token: tokenStr}, nil
}

func ListTokens() ([]APIToken, error) {
	rows, err := postgres.DB.Query(
		"SELECT id, name, token, created_at FROM api_tokens ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, fmt.Errorf("erro ao listar tokens: %w", err)
	}
	defer rows.Close()

	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.Token, &t.CreatedAt); err != nil {
			continue
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func DeleteToken(id string) error {
	_, err := postgres.DB.Exec("DELETE FROM api_tokens WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("erro ao deletar token: %w", err)
	}
	return nil
}
