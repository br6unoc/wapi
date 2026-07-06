package transcriber

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"time"

	"botwapp/config"
	"botwapp/store/postgres"
)

type transcribeResponse struct {
	Text string `json:"text"`
}

// Transcribe tenta Groq primeiro; se falhar ou não houver chave, usa Whisper local.
func Transcribe(audioData []byte, filename string) (string, error) {
	groqKey, _ := postgres.GetSetting("groq_api_key")
	if groqKey != "" {
		text, err := transcribeGroq(audioData, groqKey)
		if err == nil {
			return text, nil
		}
		log.Printf("[transcriber] Groq falhou, usando Whisper local: %v", err)
	}
	return transcribeWhisper(audioData)
}

func transcribeGroq(audioData []byte, apiKey string) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.ogg")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, bytes.NewReader(audioData)); err != nil {
		return "", err
	}
	writer.WriteField("model", "whisper-large-v3-turbo")
	writer.WriteField("language", "pt")
	writer.WriteField("response_format", "json")
	writer.Close()

	req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/audio/transcriptions", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro na chamada Groq: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Groq status %d: %s", resp.StatusCode, string(b))
	}

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("erro ao decodificar resposta Groq: %w", err)
	}
	return result.Text, nil
}

func transcribeWhisper(audioData []byte) (string, error) {
	tmpOgg, err := os.CreateTemp("", "audio-*.ogg")
	if err != nil {
		return "", fmt.Errorf("erro ao criar temp ogg: %w", err)
	}
	defer os.Remove(tmpOgg.Name())

	if _, err := tmpOgg.Write(audioData); err != nil {
		return "", fmt.Errorf("erro ao escrever ogg: %w", err)
	}
	tmpOgg.Close()

	tmpWav, err := os.CreateTemp("", "audio-*.wav")
	if err != nil {
		return "", fmt.Errorf("erro ao criar temp wav: %w", err)
	}
	defer os.Remove(tmpWav.Name())
	tmpWav.Close()

	cmd := exec.Command("ffmpeg", "-y", "-i", tmpOgg.Name(), "-ar", "16000", "-ac", "1", "-f", "wav", tmpWav.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("erro no ffmpeg: %w — %s", err, string(out))
	}

	wavData, err := os.ReadFile(tmpWav.Name())
	if err != nil {
		return "", fmt.Errorf("erro ao ler wav: %w", err)
	}

	url := config.App.WhisperURL + "/inference"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("erro ao criar form: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(wavData)); err != nil {
		return "", fmt.Errorf("erro ao copiar wav: %w", err)
	}
	writer.WriteField("language", "pt")
	writer.WriteField("response_format", "json")
	writer.Close()

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return "", fmt.Errorf("erro ao criar request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao chamar whisper: %w", err)
	}
	defer resp.Body.Close()

	var result transcribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("erro ao decodificar resposta: %w", err)
	}
	return result.Text, nil
}
