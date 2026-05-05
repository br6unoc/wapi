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
	"wapi/config"
)

type whisperResponse struct {
	Text string `json:"text"`
}

func Transcribe(audioData []byte, filename string) (string, error) {
	// Parâmetros agora são enviados via Query String conforme especificado no OpenAPI do onerahmet/openai-whisper-asr-webservice
	url := fmt.Sprintf("%s/asr?encode=true&task=transcribe&language=pt&output=json", config.App.WhisperURL)

	log.Printf("[WHISPER] Iniciando transcrição de %d bytes", len(audioData))

	// Salva o OGG em arquivo temporário
	tmpOgg, err := os.CreateTemp("", "audio-*.ogg")
	if err != nil {
		log.Printf("[WHISPER-ERROR] Falha ao criar temp ogg: %v", err)
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

	// O Whisper ASR Webservice aceita áudio direto, mas converter para wav 16k mono é mais seguro
	cmd := exec.Command("ffmpeg", "-y", "-i", tmpOgg.Name(), "-ar", "16000", "-ac", "1", "-f", "wav", tmpWav.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[WHISPER-ERROR] FFmpeg falhou: %v - Saída: %s", err, string(out))
		return "", fmt.Errorf("erro no ffmpeg: %w — %s", err, string(out))
	}
	log.Printf("[WHISPER] Áudio convertido para WAV com sucesso")

	// Lê o WAV convertido
	wavData, err := os.ReadFile(tmpWav.Name())
	if err != nil {
		return "", fmt.Errorf("erro ao ler wav: %w", err)
	}

	// Envia para o Whisper
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("audio_file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("erro ao criar form: %w", err)
	}

	if _, err := io.Copy(part, bytes.NewReader(wavData)); err != nil {
		return "", fmt.Errorf("erro ao copiar wav: %w", err)
	}

	writer.Close()

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return "", fmt.Errorf("erro ao criar request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	log.Printf("[WHISPER] Chamando API em: %s", url)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[WHISPER-ERROR] Falha na chamada HTTP: %v", err)
		return "", fmt.Errorf("erro ao chamar whisper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyErr, _ := io.ReadAll(resp.Body)
		log.Printf("[WHISPER-ERROR] Status %d: %s", resp.StatusCode, string(bodyErr))
		return "", fmt.Errorf("whisper retornou status %d", resp.StatusCode)
	}

	var result whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[WHISPER-ERROR] Falha ao decodificar JSON: %v", err)
		return "", fmt.Errorf("erro ao decodificar resposta: %w", err)
	}

	log.Printf("[WHISPER-SUCCESS] Transcrição concluída: %s", result.Text)
	return result.Text, nil
}
