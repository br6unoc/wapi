package aiagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config holds the configuration for the AI provider.
type Config struct {
	Provider string // "openai" | "groq"
	Model    string
	APIKey   string
}

// Message represents a single turn in the conversation history.
type Message struct {
	Role    string // "user" | "assistant"
	Content string
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

// Chat sends history + system prompt to the AI provider and returns the model's response.
func Chat(cfg Config, systemPrompt string, history []Message) (string, error) {
	var endpoint string
	switch cfg.Provider {
	case "openai":
		endpoint = "https://api.openai.com/v1/chat/completions"
	case "groq":
		endpoint = "https://api.groq.com/openai/v1/chat/completions"
	default:
		return "", fmt.Errorf("provedor desconhecido: %s", cfg.Provider)
	}

	messages := make([]chatMessage, 0, len(history)+1)
	messages = append(messages, chatMessage{Role: "system", Content: systemPrompt})
	for _, m := range history {
		messages = append(messages, chatMessage{Role: m.Role, Content: m.Content})
	}

	payload := chatRequest{
		Model:    cfg.Model,
		Messages: messages,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("erro ao serializar payload: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("erro ao criar request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro na requisição HTTP: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("erro ao ler resposta: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI API erro %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("erro ao desserializar resposta: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", errors.New("resposta da AI sem choices")
	}

	return chatResp.Choices[0].Message.Content, nil
}
