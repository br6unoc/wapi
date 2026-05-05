package auth

import (
	"errors"
	"fmt"
	"os"
	"time"
	"wapi/config"
	"wapi/internal/model"
	"wapi/store/postgres"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Claims struct {
	UserID    string `json:"user_id"`
	CompanyID string `json:"company_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	jwt.RegisteredClaims
}

func Login(username, password string) (string, error) {
	var user model.User
	// Busca por username ou email
	result := postgres.GORM.Where("username = ? OR email = ?", username, username).First(&user)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", errors.New("usuário não encontrado")
		}
		return "", fmt.Errorf("erro ao buscar usuário: %w", result.Error)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", errors.New("senha incorreta")
	}

	return generateToken(user)
}

func generateToken(user model.User) (string, error) {
	claims := Claims{
		UserID:    user.ID.String(),
		CompanyID: user.CompanyID.String(),
		Username:  user.Username,
		Role:      user.Role,
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
	var count int64
	postgres.GORM.Model(&model.User{}).Where("role = ?", "SUPER_ADMIN").Count(&count)

	if count > 0 {
		return nil
	}

	// Cria a empresa padrão para o Super Admin
	company := model.Company{
		Name:       "Super Admin Corp",
		AdminEmail: "admin@admin.com",
		Status:     "Ativo",
		ExpiryDate: time.Now().AddDate(100, 0, 0), // Expira em 100 anos
	}

	if err := postgres.GORM.Create(&company).Error; err != nil {
		return fmt.Errorf("erro ao criar empresa admin: %w", err)
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("erro ao gerar hash: %w", err)
	}

	user := model.User{
		CompanyID:    company.ID,
		Username:     username,
		Email:        "admin@admin.com",
		PasswordHash: string(hashed),
		Role:         "SUPER_ADMIN",
	}

	if err := postgres.GORM.Create(&user).Error; err != nil {
		return fmt.Errorf("erro ao criar admin: %w", err)
	}

	return nil
}

// SeedFromEnv é chamado durante a inicialização para criar o usuário master do SetupOrion
func SeedFromEnv() error {
	email := os.Getenv("INITIAL_ADMIN_EMAIL")
	password := os.Getenv("INITIAL_ADMIN_PASSWORD")

	if email == "" || password == "" {
		return nil // Se não houver variaveis, segue a vida normal
	}

	var count int64
	postgres.GORM.Model(&model.User{}).Where("role = ?", "SUPER_ADMIN").Count(&count)
	if count > 0 {
		return nil // Se já existe um admin, não tenta criar de novo
	}

	company := model.Company{
		Name:       "Super Admin Corp",
		AdminEmail: email,
		Status:     "Ativo",
		ExpiryDate: time.Now().AddDate(100, 0, 0), // 100 anos
	}

	if err := postgres.GORM.Create(&company).Error; err != nil {
		return fmt.Errorf("erro ao criar empresa via seed: %w", err)
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("erro ao gerar hash via seed: %w", err)
	}

	user := model.User{
		CompanyID:    company.ID,
		Username:     email,
		Email:        email,
		PasswordHash: string(hashed),
		Role:         "SUPER_ADMIN",
	}

	if err := postgres.GORM.Create(&user).Error; err != nil {
		return fmt.Errorf("erro ao criar admin via seed: %w", err)
	}

	fmt.Printf("✅ Seed: Usuário master %s criado com sucesso.\n", email)
	return nil
}

func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}
