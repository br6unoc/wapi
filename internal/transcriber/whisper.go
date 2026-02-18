package transcriber

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"time"
	"wapi/config"
)

type whisperResponse struct {
	Text string `json:"text"`
}

func Transcribe(audioData []byte, filename string) (string, error) {
	// Salva o OGG em arquivo temporário
	tmpOgg, err := os.CreateTemp("", "audio-*.ogg")
	if err != nil {
		return "", fmt.Errorf("erro ao criar temp ogg: %w", err)
	}
	defer os.Remove(tmpOgg.Name())

	if _, err := tmpOgg.Write(audioData); err != nil {
		return "", fmt.Errorf("erro ao escrever ogg: %w", err)
	}
	tmpOgg.Close()

	// Converte OGG para WAV com FFmpeg
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

	// Lê o WAV convertido
	wavData, err := os.ReadFile(tmpWav.Name())
	if err != nil {
		return "", fmt.Errorf("erro ao ler wav: %w", err)
	}

	// Envia para o Whisper
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

	var result whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	return result.Text, nil
}
