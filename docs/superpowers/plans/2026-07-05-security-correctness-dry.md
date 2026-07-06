# Security, Correctness & DRY Fix — Findings 3–10

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Corrigir os findings 3–10 do relatório de code review: SSRF, data race em Connect(), nil-deref em Passkey, sendWebhook sem timeout, ghost message, rotas passkey atrás de JWT errado, e violações DRY.

**Architecture:** Todas as correções são em `internal/instance/manager.go`, `internal/handler/instance.go`, e `cmd/server/main.go`. Nenhuma nova dependência externa — só stdlib.

**Tech Stack:** Go 1.25, Gin, net/http, sync, context. Build via Docker: `docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"`

## Global Constraints

- Nenhuma dependência nova no go.mod
- Não alterar assinaturas públicas de métodos (ex: `Connect()`, `sendWebhook()`, `keepAlive()`)
- Build deve terminar com `EXIT: 0` após cada task
- Verificar `EXIT: $LASTEXITCODE` com PowerShell após cada build

---

## Mapa de Arquivos

| Arquivo | Tasks que o modificam |
|---|---|
| `internal/instance/manager.go` | Task 1 (SSRF+timeout), Task 2 (data race+goroutine), Task 3 (ghost message), Task 6 (DRY+goroutine leak) |
| `internal/handler/instance.go` | Task 4 (nil-deref passkey) |
| `cmd/server/main.go` | Task 5 (passkey fora de JWT) |
| `internal/handler/sync.go` | Task 6 (DRY wa_sync loop) |

---

## Task 1 — SSRF via WebhookURL + sendWebhook sem timeout (Findings 3 e 8)

**Problema:** `sendWebhook` chama `http.Post(inst.WebhookURL, ...)` com cliente padrão (sem timeout) e sem validar o host. Um atacante pode apontar para `169.254.169.254` (AWS metadata) ou endereços RFC-1918 para SSRF.

**Arquivos:**
- Modify: `internal/instance/manager.go:856-868`

**Interfaces:**
- `sendWebhook` continua sendo método privado de `*Instance` sem mudança de assinatura
- `UpdateWebhook` em `handler/instance.go` continua aceitando qualquer string — a validação ocorre no momento do envio, não no cadastro

- [ ] **Step 1: Adicionar função de validação de URL e cliente com timeout**

Localizar a função `sendWebhook` em `manager.go` (linha ~856) e substituir por:

```go
var webhookClient = &http.Client{Timeout: 10 * time.Second}

func isWebhookURLSafe(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if ip == nil {
		// hostname — deixa passar (DNS resolution no momento do envio)
		return true
	}
	// bloqueia loopback, RFC-1918, link-local, APIPA
	blocked := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
	}
	for _, cidr := range blocked {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil && network.Contains(ip) {
			return false
		}
	}
	return true
}

func (inst *Instance) sendWebhook(msgData map[string]interface{}) {
	if inst.WebhookURL == "" {
		return
	}
	if !isWebhookURLSafe(inst.WebhookURL) {
		log.Printf("[WEBHOOK] URL bloqueada por política SSRF: %s", inst.WebhookURL)
		return
	}
	payload := map[string]interface{}{
		"instance": inst.Name,
		"data":     msgData,
	}
	jsonBytes, _ := json.Marshal(payload)
	resp, err := webhookClient.Post(inst.WebhookURL, "application/json", bytes.NewReader(jsonBytes))
	if err != nil {
		log.Printf("[WEBHOOK] Erro ao enviar para %s: %v", inst.WebhookURL, err)
		return
	}
	resp.Body.Close()
}
```

- [ ] **Step 2: Adicionar imports `net` e `net/url` ao bloco de imports do arquivo**

O arquivo já importa `net/http`. Adicionar `"net"` e `"net/url"` ao bloco de imports existente.

- [ ] **Step 3: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 2 — Data race em Connect() + goroutine keepAlive cascade (Finding 6)

**Problema:**
1. `Connect()` (linha 252–255) escreve `inst.ctx` e `inst.cancel` sem lock enquanto `keepAlive()` lê `inst.ctx.Done()` sem lock → data race.
2. `Connect()` chama `go inst.keepAlive()` sempre que é invocado. `autoReconnect()` chama `Connect()` até 3 vezes → 3 goroutines `keepAlive` rodando simultaneamente, cada uma enviando presence e disparando reconexões.

**Fix:**
1. Proteger leitura/escrita de `inst.ctx` e `inst.cancel` com `inst.stateMu`.
2. Adicionar campo `inst.keepAliveDone chan struct{}` para sinalizar à goroutine anterior que deve parar, e só lançar nova depois.

**Arquivos:**
- Modify: `internal/instance/manager.go` — struct `Instance`, `Connect()`, `keepAlive()`

- [ ] **Step 1: Adicionar campo `keepAliveDone` na struct Instance**

No bloco da struct (linha ~33):

```go
type Instance struct {
    // ... campos existentes ...
    keepAliveDone chan struct{} // fechado para sinalizar parada do keepAlive anterior
}
```

- [ ] **Step 2: Inicializar o campo em NewInstance**

Localizar onde `inst` é construído em `NewInstance` (linha ~168) e adicionar:
```go
keepAliveDone: make(chan struct{}),
```

- [ ] **Step 3: Proteger ctx/cancel com stateMu em Connect()**

Substituir o trecho atual (linha 252–255):
```go
inst.cancel()
ctx, cancel := context.WithCancel(context.Background())
inst.ctx = ctx
inst.cancel = cancel
```

Por:
```go
// Para o keepAlive anterior sinalizando pelo canal
inst.stateMu.Lock()
oldDone := inst.keepAliveDone
inst.keepAliveDone = make(chan struct{})
inst.cancel()
ctx, cancel := context.WithCancel(context.Background())
inst.ctx = ctx
inst.cancel = cancel
newDone := inst.keepAliveDone
inst.stateMu.Unlock()

// Aguarda goroutine anterior terminar (com timeout para não bloquear Connect)
select {
case <-oldDone:
case <-time.After(2 * time.Second):
}

go inst.keepAliveLoop(ctx, newDone)
```

Remover a linha `go inst.keepAlive()` que estava no final de `Connect()` (pois foi substituída acima).

- [ ] **Step 4: Renomear keepAlive para keepAliveLoop e usar ctx/done recebidos por parâmetro**

Substituir a assinatura e leitura de `inst.ctx` em `keepAlive`:

```go
func (inst *Instance) keepAliveLoop(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if inst.WAClient.IsConnected() {
				inst.WAClient.SendPresence(context.Background(), types.PresenceAvailable)
				if inst.GetStatus() != "connected" && inst.WAClient.Store.ID != nil {
					phone := inst.WAClient.Store.ID.User
					inst.SetState("connected", phone)
					log.Printf("[KEEPALIVE] Instance %s reconnected - Phone: %s", inst.Name, phone)
					p, _ := json.Marshal(map[string]interface{}{"event": "connected", "data": map[string]string{"phone": phone}})
					inst.BroadcastSSE(string(p))
					inst.saveStatusToDB()
				}
			} else {
				if inst.GetStatus() == "connected" {
					inst.SetState("disconnected", "")
					log.Printf("[KEEPALIVE] Instance %s disconnected, iniciando auto-reconexão...", inst.Name)
					p, _ := json.Marshal(map[string]interface{}{"event": "disconnected", "data": map[string]interface{}{}})
					inst.BroadcastSSE(string(p))
					inst.saveStatusToDB()
					go inst.autoReconnect()
				}
			}
		}
	}
}
```

Remover a função `keepAlive()` original.

- [ ] **Step 5: Ler inst.ctx com lock em autoReconnect onde necessário**

`autoReconnect()` lê `inst.ctx.Done()` diretamente (linha 356). Substituir por receber o ctx como parâmetro ou ler com lock:

```go
func (inst *Instance) autoReconnect() {
	if inst.WAClient == nil || inst.WAClient.Store.ID == nil {
		log.Printf("[RECONNECT] Instância %s sem sessão válida, aguardando novo QR", inst.Name)
		return
	}
	inst.stateMu.RLock()
	ctx := inst.ctx
	inst.stateMu.RUnlock()

	for attempt := 1; attempt <= 3; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt*30) * time.Second):
		}
		// ... resto igual ...
	}
}
```

- [ ] **Step 6: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 3 — Ghost message: INSERT falha silenciosamente (Finding 9)

**Problema:** Em `manager.go:1060`, `QueryRow(INSERT...).Scan(&createdAt)` descarta o erro — a mensagem é enviada no WhatsApp mas não salva no banco. O usuário perde o histórico.

**Arquivos:**
- Modify: `internal/instance/manager.go:1058-1064`

- [ ] **Step 1: Localizar e corrigir o INSERT sem checagem**

Substituir (linha ~1060):
```go
outID := uuid.New().String()
var createdAt string
postgres.DB.QueryRow(
    `INSERT INTO messages (id, instance_id, contact_id, direction, content, type, wa_message_id, media_path, media_name)
     VALUES ($1, $2, $3, 'out', $4, 'text', '', '', '') RETURNING created_at`,
    outID, inst.ID, contactID, aiResp,
).Scan(&createdAt)
```

Por:
```go
outID := uuid.New().String()
var createdAt string
if err := postgres.DB.QueryRow(
    `INSERT INTO messages (id, instance_id, contact_id, direction, content, type, wa_message_id, media_path, media_name)
     VALUES ($1, $2, $3, 'out', $4, 'text', '', '', '') RETURNING created_at`,
    outID, inst.ID, contactID, aiResp,
).Scan(&createdAt); err != nil {
    log.Printf("[DB] Falha ao persistir mensagem AI para contato %s: %v", contactID, err)
    return
}
```

> **Por que `return`:** Se a mensagem não foi salva no banco, não deve ser enviada via WhatsApp — evita ghost message onde o usuário recebe mas o histórico some.

- [ ] **Step 2: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 4 — Nil-deref em PasskeyResponse e PasskeyConfirm (Finding 7)

**Problema:** `inst.WAClient.SendPasskeyResponse()` é chamado sem nil-check em `handler/instance.go:400`. Se a instância estiver desconectada, `WAClient` é nil → panic.

**Arquivos:**
- Modify: `internal/handler/instance.go:388-423`

- [ ] **Step 1: Adicionar nil-check em PasskeyResponse**

Substituir:
```go
func PasskeyResponse(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}
	var resp types.WebAuthnResponse
	if err := c.ShouldBindJSON(&resp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}
	if err := inst.WAClient.SendPasskeyResponse(c.Request.Context(), &resp); err != nil {
```

Por:
```go
func PasskeyResponse(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}
	if inst.WAClient == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "instância não está conectada"})
		return
	}
	var resp types.WebAuthnResponse
	if err := c.ShouldBindJSON(&resp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "dados inválidos"})
		return
	}
	if err := inst.WAClient.SendPasskeyResponse(c.Request.Context(), &resp); err != nil {
```

- [ ] **Step 2: Adicionar nil-check em PasskeyConfirm**

Substituir:
```go
func PasskeyConfirm(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}
	if err := inst.WAClient.SendPasskeyConfirmation(c.Request.Context()); err != nil {
```

Por:
```go
func PasskeyConfirm(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}
	if inst.WAClient == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "instância não está conectada"})
		return
	}
	if err := inst.WAClient.SendPasskeyConfirmation(c.Request.Context()); err != nil {
```

- [ ] **Step 3: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 5 — Rotas passkey atrás de JWT errado (Finding 10)

**Problema:** Passkey pairing ocorre antes do usuário ter sessão autenticada no WhatsApp. As rotas estão no grupo `instances` que exige JWT — se o token expirar no meio do handshake, retorna 401 e o pairing falha. A solução correta: mover para grupo autenticado apenas por API Key (como as rotas de envio).

**Arquivos:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Localizar os grupos de rotas**

Identificar onde estão os grupos `instances` (JWT) e o grupo com `APIKeyMiddleware`. As rotas de passkey estão atualmente em:
```go
instances.POST("/:name/passkey/response", handler.PasskeyResponse)
instances.POST("/:name/passkey/confirm", handler.PasskeyConfirm)
```

- [ ] **Step 2: Mover rotas para o grupo API Key**

Remover as duas linhas acima do grupo `instances` e adicioná-las ao grupo que usa `APIKeyMiddleware`:
```go
api := r.Group("/api/instances/:name", handler.APIKeyMiddleware())
{
    // rotas existentes ...
    api.POST("/passkey/response", handler.PasskeyResponse)
    api.POST("/passkey/confirm", handler.PasskeyConfirm)
}
```

> **Nota:** `PasskeyResponse` e `PasskeyConfirm` já usam `getInstanceForUser(c, name)` que funciona sem `company_id` quando chamado sem JWT (retorna a instância pelo nome). O `APIKeyMiddleware` já seta `c.Set("instance", inst)` mas os handlers não o usam — são compatíveis.

- [ ] **Step 3: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 6 — DRY: payload new_message + wa_sync loop + SaveStatusToDB wrapper + goroutine leak (Extras)

**Problema (4 sub-itens):**

**6a.** `wa_sync:*` loop idêntico em `APISyncStats` e `WebSync` em `sync.go`.

**6b.** `SaveStatusToDB` é wrapper vazio de `saveStatusToDB` — exportado sem uso real.

**6c.** Payload `new_message` construído de forma idêntica em 5 lugares em `manager.go` — quando o schema muda, precisa ser atualizado em 5 pontos.

**6d.** Goroutine leak: `sendWebhook` já lançado com `go` em 5 lugares — cada mensagem cria uma goroutine. Com `http.Client{Timeout: 10s}` (Task 1) o leak é limitado mas pode acumular sob carga. Adicionar semáforo.

**Arquivos:**
- Modify: `internal/handler/sync.go` (6a)
- Modify: `internal/instance/manager.go` (6b, 6c, 6d)

- [ ] **Step 1 (6a): Extrair função readWASyncStats em sync.go**

Antes das funções `APISyncStats` e `WebSync`, adicionar helper:
```go
type waSyncData struct {
	InstanceID   string
	InstanceName string
	Total        int
	Individual   int
	Groups       int
	SyncedAt     string
	SyncType     string
}

func readWASyncStats(companyID string) []waSyncData {
	toInt := func(s string) int { n, _ := strconv.Atoi(s); return n }
	var result []waSyncData
	for _, inst := range instance.Global.GetByCompany(companyID) {
		total, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":total")
		ind, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":individual")
		grp, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":groups")
		at, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":at")
		tp, _ := postgres.GetSetting("wa_sync:" + inst.ID + ":type")
		result = append(result, waSyncData{
			InstanceID:   inst.ID,
			InstanceName: inst.Name,
			Total:        toInt(total),
			Individual:   toInt(ind),
			Groups:       toInt(grp),
			SyncedAt:     at,
			SyncType:     tp,
		})
	}
	return result
}
```

Substituir os loops duplicados em `APISyncStats` e `WebSync` por chamada a `readWASyncStats(companyID)`.

- [ ] **Step 2 (6b): Remover SaveStatusToDB exportado**

Em `manager.go`, remover:
```go
// SaveStatusToDB salva o status atual no banco (exportado para uso externo)
func (inst *Instance) SaveStatusToDB() {
	inst.saveStatusToDB()
}
```

Verificar com grep se `SaveStatusToDB` (maiúsculo) é chamado em algum outro arquivo. Se não for, remover sem substituição.

```
grep -r "SaveStatusToDB" internal/ cmd/
```

Se houver chamadas externas, substituí-las por `saveStatusToDB` diretamente (pois estão no mesmo pacote) ou manter a função exportada.

- [ ] **Step 3 (6c): Extrair buildNewMessagePayload em manager.go**

Adicionar função helper:
```go
func buildNewMessagePayload(inst *Instance, contactID, phone, name, direction, content, msgType, mediaPath, mediaName, waID, createdAt string) map[string]interface{} {
	return map[string]interface{}{
		"event": "new_message",
		"data": map[string]interface{}{
			"id":            waID,
			"instance_id":   inst.ID,
			"instance_name": inst.Name,
			"contact_id":    contactID,
			"phone":         phone,
			"name":          name,
			"direction":     direction,
			"content":       content,
			"type":          msgType,
			"media_path":    mediaPath,
			"media_name":    mediaName,
			"created_at":    createdAt,
		},
	}
}
```

Substituir os 5 pontos onde o payload é construído manualmente pela chamada a `buildNewMessagePayload(...)`.

- [ ] **Step 4 (6d): Adicionar semáforo para limitar goroutines de sendWebhook**

Logo após a declaração do `webhookClient` (Task 1), adicionar:
```go
var webhookSem = make(chan struct{}, 20) // máx 20 webhooks simultâneos
```

Substituir os `go inst.sendWebhook(msgData)` por:
```go
select {
case webhookSem <- struct{}{}:
    go func(data map[string]interface{}) {
        defer func() { <-webhookSem }()
        inst.sendWebhook(data)
    }(msgData)
default:
    log.Printf("[WEBHOOK] Semáforo cheio, webhook descartado para %s", inst.Name)
}
```

- [ ] **Step 5: Compilar e verificar final**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0` — compilação limpa, todos os findings 3–10 corrigidos.

---

## Checklist Final

- [ ] `grep -r "http\.Post" internal/` → zero ocorrências (substituído por `webhookClient.Post`)
- [ ] `grep -r "\.keepAlive()" internal/` → zero ocorrências (substituído por `keepAliveLoop`)
- [ ] `grep -n "SaveStatusToDB" internal/ cmd/` → zero ou apenas a definição removida
- [ ] Build final: `EXIT: 0`
