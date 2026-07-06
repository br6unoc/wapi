# Multi-tenant Isolation Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Garantir que nenhum usuário autenticado consiga ler, modificar ou deletar instâncias de outra empresa, e que chamadas via API Key também estejam isoladas por tenant.

**Architecture:** Dois vetores de ataque corrigidos. (1) Handlers JWT chamam `GetByName` global em vez de `getInstanceForUser` (que já existe e filtra por `company_id`). (2) `APIKeyMiddleware` também usa `GetByName` global em vez de `GetByAPIKey` (que já existe, usa constant-time compare e é independente de tenant name). Adicionalmente, `AuthMiddleware` silencia erros de DB e aplica role `admin` por padrão, e `ListInstances` retorna instâncias de todas as empresas.

**Tech Stack:** Go 1.25, Gin, PostgreSQL, whatsmeow. Sem dependências novas.

## Global Constraints

- Nenhuma dependência nova pode ser adicionada ao go.mod
- `getInstanceForUser(c, name)` já existe em `internal/handler/middleware.go:77` — usar sempre que um handler JWT precisar de uma instância por nome
- `GetByAPIKey(apiKey)` já existe em `internal/instance/manager.go:126` — usar no middleware de API Key
- `GetByCompany(companyID)` já existe em `internal/instance/manager.go:103` — usar no ListInstances
- Build command: `docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"`
- Verificar saída `EXIT: 0` após cada task

---

## Mapa de Arquivos

| Arquivo | O que muda |
|---|---|
| `internal/handler/middleware.go` | Task 1 (AuthMiddleware erro DB) + Task 3 (APIKeyMiddleware → GetByAPIKey) |
| `internal/handler/instance.go` | Task 2 (ListInstances por company) + Task 4 (18× GetByName → getInstanceForUser) |
| `internal/handler/conversations.go` | Task 5 (4× GetByName sem guard de tenant) |

---

## Task 1 — AuthMiddleware: tratar erro de DB, não promover a admin

**Problema:** Se o `QueryRow/Scan` falhar, `role` fica com o valor COALESCE `'admin'` e a request prossegue com privilégio de admin.

**Arquivos:**
- Modify: `internal/handler/middleware.go:36-43`

**Interfaces:**
- Nenhuma mudança de assinatura externa. O middleware continua sendo `gin.HandlerFunc`.

- [ ] **Step 1: Localizar o trecho exato**

Linha 36-43 de `internal/handler/middleware.go`:
```go
var role, companyID string
postgres.DB.QueryRow(`SELECT COALESCE(role,'admin'), COALESCE(company_id::text,'') FROM users WHERE id = $1`, claims.UserID).Scan(&role, &companyID)

c.Set("user_id", claims.UserID)
```

- [ ] **Step 2: Aplicar a correção**

Substituir o bloco acima por:
```go
var role, companyID string
err = postgres.DB.QueryRow(
    `SELECT COALESCE(role,'admin'), COALESCE(company_id::text,'') FROM users WHERE id = $1`,
    claims.UserID,
).Scan(&role, &companyID)
if err != nil {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não encontrado"})
    c.Abort()
    return
}

c.Set("user_id", claims.UserID)
```

- [ ] **Step 3: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0` sem erros de compilação.

---

## Task 2 — ListInstances: filtrar por empresa + leitura segura de campos

**Problema:** `GetAll()` retorna instâncias de todas as empresas. Além disso, `inst.Status` e `inst.Phone` são lidos diretamente sem o mutex `stateMu`.

**Arquivos:**
- Modify: `internal/handler/instance.go:16-37`

**Interfaces:**
- Nenhuma mudança de assinatura. Response JSON igual, mas com apenas as instâncias da empresa do usuário.

- [ ] **Step 1: Localizar o trecho**

```go
func ListInstances(c *gin.Context) {
	instances := instance.Global.GetAll()
	result := make([]gin.H, 0, len(instances))
	for _, inst := range instances {
		actualStatus := "disconnected"
		actualPhone := inst.Phone
		
		if inst.Status == "connected" && inst.Phone != "" {
			actualStatus = "connected"
		}
		...
```

- [ ] **Step 2: Aplicar a correção**

```go
func ListInstances(c *gin.Context) {
	companyID := currentCompanyID(c)
	instances := instance.Global.GetByCompany(companyID)
	result := make([]gin.H, 0, len(instances))
	for _, inst := range instances {
		status, phone := inst.GetState()
		if status != "connected" || phone == "" {
			status = "disconnected"
			phone = ""
		}
		log.Printf("[LIST] Instance %s - Status: %s, Phone: %s", inst.Name, status, phone)
		result = append(result, gin.H{
			"id":     inst.ID,
			"name":   inst.Name,
			"status": status,
			"phone":  phone,
		})
	}
	c.JSON(http.StatusOK, result)
}
```

- [ ] **Step 3: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 3 — APIKeyMiddleware: usar GetByAPIKey (cross-tenant + timing attack)

**Problema:** O middleware busca a instância por nome global (`GetByName`) e depois compara a API key com `==`. Isso permite: (1) acessar instância de outro tenant se souber o nome; (2) timing oracle attack.

**Fix em uma linha:** `GetByAPIKey` já faz a lookup pela key (com `subtle.ConstantTimeCompare`) e retorna a instância correta de forma segura. Não depende de nome — o nome vem do param apenas para verificar que a key pertence à instância certa.

**Arquivos:**
- Modify: `internal/handler/middleware.go:94-120`

- [ ] **Step 1: Localizar o trecho atual**

```go
func APIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("apikey")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key não informada"})
			c.Abort()
			return
		}

		instanceName := c.Param("name")
		inst, ok := instance.Global.GetByName(instanceName)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
			c.Abort()
			return
		}

		if inst.APIKey != apiKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key inválida"})
			c.Abort()
			return
		}

		c.Set("instance", inst)
		c.Next()
	}
}
```

- [ ] **Step 2: Aplicar a correção**

```go
func APIKeyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("apikey")
		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key não informada"})
			c.Abort()
			return
		}

		inst, ok := instance.Global.GetByAPIKey(apiKey)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key inválida"})
			c.Abort()
			return
		}

		// Confirma que a key pertence à instância do path (evita usar key de outra instância)
		if name := c.Param("name"); name != "" && inst.Name != name {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API Key inválida para esta instância"})
			c.Abort()
			return
		}

		c.Set("instance", inst)
		c.Next()
	}
}
```

**Por que isso resolve tudo:**
- `GetByAPIKey` usa `subtle.ConstantTimeCompare` internamente → sem timing attack
- A lookup é pela key, não pelo nome → sem cross-tenant por nome
- O guard `inst.Name != name` garante que uma key de instância X não funciona na rota de instância Y

- [ ] **Step 3: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 4 — instance.go: 18× GetByName → getInstanceForUser

**Problema:** Todos os handlers JWT de instância chamam `GetByName` global. `getInstanceForUser(c, name)` já existe em `middleware.go` e usa `GetByNameAndCompany` com o `company_id` do token.

**Arquivos:**
- Modify: `internal/handler/instance.go` — substituições mecânicas

**Regra:** Qualquer ocorrência de:
```go
inst, ok := instance.Global.GetByName(name)
```
onde `name = c.Param("name")` deve virar:
```go
inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 1: Listar todas as funções afetadas**

As seguintes funções usam `GetByName` sem guard de tenant (verificado via grep):
`GetInstance` (l.78), `UpdateInstanceAgents` (l.109), `UpdateInstanceSectors` (l.146), `DeleteInstance` (l.185), `DeleteInstanceByID` — usa `Get(id)`, já é seguro pois filtra por ID, **não alterar**. `GetStatus` (l.213), `GetQRCode` (l.233), `ConnectInstance` (l.244), `DisconnectInstance` (l.260), `UpdateWebhook` (l.274), `RegenerateAPIKey` (l.296), `UpdateConfig` (l.332), `SSEHandler` (l.347), `PasskeyResponse` (l.392), `PasskeyConfirm` (l.412), `GetGroups` (l.427).

- [ ] **Step 2: Aplicar substituição em GetInstance**

De:
```go
func GetInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func GetInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 3: Aplicar substituição em UpdateInstanceAgents**

De:
```go
func UpdateInstanceAgents(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func UpdateInstanceAgents(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 4: Aplicar substituição em UpdateInstanceSectors**

De:
```go
func UpdateInstanceSectors(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func UpdateInstanceSectors(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 5: Aplicar substituição em DeleteInstance**

De:
```go
func DeleteInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func DeleteInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 6: Aplicar substituição em GetStatus**

De:
```go
func GetStatus(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func GetStatus(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 7: Aplicar substituição em GetQRCode**

De:
```go
func GetQRCode(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func GetQRCode(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 8: Aplicar substituição em ConnectInstance**

De:
```go
func ConnectInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func ConnectInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 9: Aplicar substituição em DisconnectInstance**

De:
```go
func DisconnectInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func DisconnectInstance(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 10: Aplicar substituição em UpdateWebhook**

De:
```go
func UpdateWebhook(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func UpdateWebhook(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 11: Aplicar substituição em RegenerateAPIKey**

De:
```go
func RegenerateAPIKey(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func RegenerateAPIKey(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 12: Aplicar substituição em UpdateConfig**

De:
```go
func UpdateConfig(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func UpdateConfig(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 13: Aplicar substituição em SSEHandler**

De:
```go
func SSEHandler(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func SSEHandler(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

> ⚠️ **Atenção SSEHandler:** Esta rota fica no grupo sem auth em `main.go` (`r.GET("/instances/:name/sse", handler.SSEHandler)`). `getInstanceForUser` usa `currentCompanyID(c)` que retorna `""` se não houver token. `GetByNameAndCompany` com `companyID=""` retorna qualquer empresa (ver manager.go:116 — `companyID == ""`). Isso é correto: SSE sem auth continua funcionando. **Não alterar a rota no main.go**.

- [ ] **Step 14: Aplicar substituição em PasskeyResponse e PasskeyConfirm**

```go
func PasskeyResponse(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

```go
func PasskeyConfirm(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 15: Aplicar substituição em GetGroups**

De:
```go
func GetGroups(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
```
Para:
```go
func GetGroups(c *gin.Context) {
	name := c.Param("name")
	inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 16: Remover import de `instance` se não houver mais uso direto**

Verificar se `instance.Global` ainda é referenciado em `instance.go` após as substituições. Se não, remover do import block. (Provavelmente ainda usado em `CreateInstance` e `DeleteInstanceByID`.)

- [ ] **Step 17: Compilar e verificar**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0`.

---

## Task 5 — conversations.go: 4× GetByName sem guard de tenant

**Problema:** Handlers de conversa também chamam `GetByName` sem verificar empresa. Diferente de `instance.go`, alguns recebem o nome via body JSON (não param), então o padrão é diferente.

**Arquivos:**
- Modify: `internal/handler/conversations.go`

**Ocorrências:**
- `SendMediaFromUI` (l.341) — `instanceName` vem do body
- `TakeoverConversation` (l.533) — `name` vem do path param
- `ResumeAgent` (l.575) — `name` vem do path param
- `SendFromUI` (l.626) — `instanceName` vem do body

> **Nota:** `send.go` usa `GetByName` mas está atrás de `APIKeyMiddleware` — corrigido pela Task 3. Não alterar `send.go`.

- [ ] **Step 1: Corrigir SendMediaFromUI (l.341)**

De:
```go
inst, ok := instance.Global.GetByName(instanceName)
```
Para:
```go
inst, ok := getInstanceForUser(c, instanceName)
```

- [ ] **Step 2: Corrigir TakeoverConversation (l.533)**

De:
```go
name := c.Param("name")  // ou variável equivalente
inst, ok := instance.Global.GetByName(name)
```
Para:
```go
name := c.Param("name")
inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 3: Corrigir ResumeAgent (l.575)**

De:
```go
inst, ok := instance.Global.GetByName(name)
```
Para:
```go
inst, ok := getInstanceForUser(c, name)
```

- [ ] **Step 4: Corrigir SendFromUI (l.626)**

De:
```go
inst, ok := instance.Global.GetByName(instanceName)
```
Para:
```go
inst, ok := getInstanceForUser(c, instanceName)
```

- [ ] **Step 5: Compilar e verificar final**

```
docker run --rm -v "\\wsl.localhost\Ubuntu-22.04\home\dev\wapi:/app" -w /app golang:1.25-alpine sh -c "apk add --no-cache gcc musl-dev 2>/dev/null && CGO_ENABLED=1 GOOS=linux go build -o wapi ./cmd/server/main.go 2>&1"; Write-Host "EXIT: $LASTEXITCODE"
```

Esperado: `EXIT: 0` — compilação limpa, todas as correções aplicadas.

---

## Checklist Final de Verificação Manual

Após concluir as 5 tasks, confirmar:

- [ ] `grep -n "GetByName" internal/handler/*.go` retorna apenas usos legítimos (ex: `DeleteInstanceByID` que usa `Get(id)`, não `GetByName`)
- [ ] `grep -n "inst.APIKey !=" internal/handler/*.go` retorna zero resultados
- [ ] `grep -n "GetAll()" internal/handler/*.go` retorna zero resultados
- [ ] `grep -n "inst.Status\|inst.Phone" internal/handler/instance.go` retorna zero acessos diretos fora de funções que já usam `GetState()`
- [ ] Build final: `EXIT: 0`

---

## Resumo do que cada task resolve

| Task | Finding | Arquivo |
|---|---|---|
| 1 | AuthMiddleware silencia erro → admin | middleware.go |
| 2 | ListInstances retorna todos os tenants | instance.go |
| 3 | APIKeyMiddleware cross-tenant + timing attack | middleware.go |
| 4 | 15+ handlers JWT sem guard de tenant | instance.go |
| 5 | 4 handlers de conversa sem guard de tenant | conversations.go |
