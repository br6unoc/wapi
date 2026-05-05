package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AIRequest struct {
	Model       string      `json:"model"`
	Messages    []AIMessage `json:"messages"`
	Temperature float64     `json:"temperature"`
}

type AIResponse struct {
	Choices []struct {
		Message AIMessage `json:"message"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func CallAI(provider, model, apiKey, systemPrompt string, messages []AIMessage, entropy float64) (string, error) {
	var baseURL string
	switch provider {
	case "openai":
		baseURL = "https://api.openai.com/v1/chat/completions"
	case "groq":
		baseURL = "https://api.groq.com/openai/v1/chat/completions"
	default:
		return "", fmt.Errorf("provedor de IA desconhecido: %s", provider)
	}

	// Insere o system prompt no início
	fullMessages := append([]AIMessage{{Role: "system", Content: systemPrompt}}, messages...)

	reqBody := AIRequest{
		Model:       model,
		Messages:    fullMessages,
		Temperature: entropy,
	}

	jsonBody, _ := json.Marshal(reqBody)
	
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", baseURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var aiErr AIResponse
		json.Unmarshal(bodyBytes, &aiErr)
		if aiErr.Error.Message != "" {
			return "", fmt.Errorf("erro da API (%s): %s", provider, aiErr.Error.Message)
		}
		return "", fmt.Errorf("erro da API (%s): status %d - %s", provider, resp.StatusCode, string(bodyBytes))
	}

	var aiResp AIResponse
	if err := json.Unmarshal(bodyBytes, &aiResp); err != nil {
		return "", err
	}

	if len(aiResp.Choices) > 0 {
		return aiResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("nenhuma resposta retornada pela IA")
}
