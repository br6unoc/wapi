# CRM WhatsApp — Parte 1: Atendimento Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transformar o WAPI em um CRM de atendimento WhatsApp inspirado no Whaticket, com suporte a filas, conversas, contatos e tela de atendimento em tempo real.

**Architecture:** O WAPI mantém seu núcleo (multi-instância, whatsmeow, envio/recebimento de mensagens). Sobre ele adicionamos: (1) modelos de CRM no banco — contatos, conversas, filas, mensagens; (2) lógica de atendimento que processa cada mensagem recebida e atualiza as conversas; (3) frontend SSR com Go templates + Alpine.js para a tela de atendimento em tempo real via SSE. O `web/index.html` atual é removido e substituído pelo sistema de templates.

**Tech Stack:** Go 1.24, Gin, PostgreSQL, whatsmeow, Go html/template, Alpine.js (CDN), SSE para tempo real

---

## Mapeamento de Arquivos

### Novos arquivos
| Arquivo | Responsabilidade |
|---------|-----------------|
| `store/migrations/002_crm.sql` | Tabelas: queues, contacts, conversations, messages (CRM) |
| `internal/crm/contact.go` | CRUD de contatos (buscar/criar por número de telefone) |
| `internal/crm/conversation.go` | CRUD de conversas (criar, atualizar status, atribuir fila/agente) |
| `internal/crm/message.go` | Salvar mensagens, listar por conversa |
| `internal/crm/queue.go` | CRUD de filas |
| `internal/handler/crm.go` | Handlers HTTP para filas, conversas, contatos |
| `internal/handler/frontend.go` | Handlers para renderizar templates HTML |
| `web/templates/layout.html` | Layout base (header, sidebar, scripts Alpine.js) |
| `web/templates/atendimento.html` | Tela principal de atendimento |
| `web/static/style.css` | CSS base do CRM |

### Arquivos modificados
| Arquivo | O que muda |
|---------|-----------|
| `store/migrations/001_init.sql` | Sem mudança (mantido) |
| `store/postgres/store.go` | Adicionar função `Migrate()` que roda 002_crm.sql |
| `internal/instance/manager.go` | `processMessage` chama `crm.HandleIncomingMessage` em goroutine |
| `cmd/server/main.go` | Registrar rotas de CRM e frontend; remover `StaticFile` do index.html |
| `store/migrations/002_crm.sql` | Criado do zero |

---

## Task 1: Migration do banco CRM

**Files:**
- Create: `store/migrations/002_crm.sql`
- Modify: `store/postgres/store.go`

- [ ] **Step 1: Criar migration SQL**

```sql
-- store/migrations/002_crm.sql
CREATE TABLE IF NOT EXISTS queues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) UNIQUE NOT NULL,
    color VARCHAR(20) DEFAULT '#2196F3',
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS contacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone VARCHAR(50) UNIQUE NOT NULL,
    name VARCHAR(255) DEFAULT '',
    profile_pic_url TEXT DEFAULT '',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    contact_id UUID NOT NULL REFERENCES contacts(id),
    queue_id UUID REFERENCES queues(id),
    assigned_user_id UUID REFERENCES users(id),
    status VARCHAR(50) DEFAULT 'pending',
    unread_count INTEGER DEFAULT 0,
    last_message TEXT DEFAULT '',
    last_message_at TIMESTAMP DEFAULT NOW(),
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(instance_id, contact_id)
);

CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    wa_message_id VARCHAR(255) DEFAULT '',
    direction VARCHAR(10) NOT NULL CHECK (direction IN ('in', 'out')),
    type VARCHAR(20) DEFAULT 'text',
    content TEXT DEFAULT '',
    media_url TEXT DEFAULT '',
    transcription TEXT DEFAULT '',
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversations_instance_id ON conversations(instance_id);
CREATE INDEX IF NOT EXISTS idx_conversations_status ON conversations(status);
CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);

-- Fila padrão
INSERT INTO queues (name, color) VALUES ('Atendimento', '#4CAF50') ON CONFLICT DO NOTHING;
```

- [ ] **Step 2: Ler o arquivo atual de migrations para não quebrar a ordem**

Arquivo: `store/postgres/store.go` — verificar como `Migrate()` está implementada.

```bash
cat /opt/wapi-refactor/store/postgres/store.go
```

- [ ] **Step 3: Atualizar store.go para executar ambas as migrations em ordem**

```go
// store/postgres/store.go — substituir a função Migrate
func Migrate() error {
	migrations := []string{
		"store/migrations/001_init.sql",
		"store/migrations/002_crm.sql",
	}
	for _, path := range migrations {
		content, err := os.ReadFile(path)
		if err != nil {
			// migration opcional ainda não existe: pular
			continue
		}
		if _, err := DB.Exec(string(content)); err != nil {
			return fmt.Errorf("erro ao executar migration %s: %w", path, err)
		}
	}
	return nil
}
```

Adicionar import `"os"` e `"fmt"` se não existirem.

- [ ] **Step 4: Compilar e verificar**

```bash
docker exec wapi-dev sh -c "cd /app && go build ./... 2>&1"
```

Esperado: sem erros.

- [ ] **Step 5: Reiniciar o servidor e verificar que as tabelas foram criadas**

```bash
docker exec wapi-dev sh -c "pkill server; cd /app && export \$(cat .env | grep -v '^#' | xargs) && ./server &" 2>/dev/null
sleep 3
docker exec vendassit-dev-postgres psql -U wapi -d wapi -c "\dt"
```

Esperado: tabelas `queues`, `contacts`, `conversations`, `messages` listadas.

- [ ] **Step 6: Commit**

```bash
cd /opt/wapi-refactor
git add store/migrations/002_crm.sql store/postgres/store.go
git commit -m "feat: add CRM database migrations (queues, contacts, conversations, messages)"
```

---

## Task 2: Pacote CRM — Contatos e Conversas

**Files:**
- Create: `internal/crm/contact.go`
- Create: `internal/crm/conversation.go`
- Create: `internal/crm/message.go`
- Create: `internal/crm/queue.go`

- [ ] **Step 1: Criar internal/crm/contact.go**

```go
package crm

import (
	"wapi/store/postgres"
	"time"
)

type Contact struct {
	ID            string
	Phone         string
	Name          string
	ProfilePicURL string
	CreatedAt     time.Time
}

func FindOrCreateContact(phone, name string) (*Contact, error) {
	c := &Contact{}
	err := postgres.DB.QueryRow(
		`SELECT id, phone, name, profile_pic_url, created_at FROM contacts WHERE phone = $1`,
		phone,
	).Scan(&c.ID, &c.Phone, &c.Name, &c.ProfilePicURL, &c.CreatedAt)

	if err == nil {
		// Atualiza nome se mudou (pushName do WhatsApp)
		if name != "" && name != c.Name {
			postgres.DB.Exec(`UPDATE contacts SET name = $1, updated_at = NOW() WHERE id = $2`, name, c.ID)
			c.Name = name
		}
		return c, nil
	}

	// Não encontrou: criar
	err = postgres.DB.QueryRow(
		`INSERT INTO contacts (phone, name) VALUES ($1, $2) RETURNING id, phone, name, profile_pic_url, created_at`,
		phone, name,
	).Scan(&c.ID, &c.Phone, &c.Name, &c.ProfilePicURL, &c.CreatedAt)
	return c, err
}
```

- [ ] **Step 2: Criar internal/crm/conversation.go**

```go
package crm

import (
	"database/sql"
	"wapi/store/postgres"
	"time"
)

type Conversation struct {
	ID             string
	InstanceID     string
	ContactID      string
	QueueID        string
	AssignedUserID string
	Status         string
	UnreadCount    int
	LastMessage    string
	LastMessageAt  time.Time
	CreatedAt      time.Time
	// Joins
	ContactPhone string
	ContactName  string
}

func FindOrCreateConversation(instanceID, contactID string) (*Conversation, error) {
	conv := &Conversation{}
	err := postgres.DB.QueryRow(
		`SELECT id, instance_id, contact_id, COALESCE(queue_id,''), COALESCE(assigned_user_id,''),
		        status, unread_count, last_message, last_message_at, created_at
		 FROM conversations WHERE instance_id = $1 AND contact_id = $2`,
		instanceID, contactID,
	).Scan(&conv.ID, &conv.InstanceID, &conv.ContactID, &conv.QueueID, &conv.AssignedUserID,
		&conv.Status, &conv.UnreadCount, &conv.LastMessage, &conv.LastMessageAt, &conv.CreatedAt)

	if err == sql.ErrNoRows {
		// Buscar fila padrão
		var defaultQueueID string
		postgres.DB.QueryRow(`SELECT id FROM queues ORDER BY created_at LIMIT 1`).Scan(&defaultQueueID)

		err = postgres.DB.QueryRow(
			`INSERT INTO conversations (instance_id, contact_id, queue_id, status)
			 VALUES ($1, $2, $3, 'pending')
			 RETURNING id, instance_id, contact_id, COALESCE(queue_id,''), COALESCE(assigned_user_id,''),
			           status, unread_count, last_message, last_message_at, created_at`,
			instanceID, contactID, defaultQueueID,
		).Scan(&conv.ID, &conv.InstanceID, &conv.ContactID, &conv.QueueID, &conv.AssignedUserID,
			&conv.Status, &conv.UnreadCount, &conv.LastMessage, &conv.LastMessageAt, &conv.CreatedAt)
	}
	return conv, err
}

func UpdateConversationLastMessage(convID, msg string) {
	postgres.DB.Exec(
		`UPDATE conversations SET last_message = $1, last_message_at = NOW(), unread_count = unread_count + 1 WHERE id = $2`,
		msg, convID,
	)
}

func ListConversations(instanceID string) ([]*Conversation, error) {
	rows, err := postgres.DB.Query(
		`SELECT c.id, c.instance_id, c.contact_id, COALESCE(c.queue_id,''), COALESCE(c.assigned_user_id,''),
		        c.status, c.unread_count, c.last_message, c.last_message_at, c.created_at,
		        ct.phone, ct.name
		 FROM conversations c
		 JOIN contacts ct ON ct.id = c.contact_id
		 WHERE c.instance_id = $1
		 ORDER BY c.last_message_at DESC`,
		instanceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []*Conversation
	for rows.Next() {
		conv := &Conversation{}
		rows.Scan(&conv.ID, &conv.InstanceID, &conv.ContactID, &conv.QueueID, &conv.AssignedUserID,
			&conv.Status, &conv.UnreadCount, &conv.LastMessage, &conv.LastMessageAt, &conv.CreatedAt,
			&conv.ContactPhone, &conv.ContactName)
		convs = append(convs, conv)
	}
	return convs, nil
}
```

- [ ] **Step 3: Criar internal/crm/message.go**

```go
package crm

import (
	"wapi/store/postgres"
	"time"
)

type Message struct {
	ID             string
	ConversationID string
	WAMessageID    string
	Direction      string
	Type           string
	Content        string
	MediaURL       string
	Transcription  string
	CreatedAt      time.Time
}

func SaveMessage(convID, waID, direction, msgType, content, transcription string) (*Message, error) {
	msg := &Message{}
	err := postgres.DB.QueryRow(
		`INSERT INTO messages (conversation_id, wa_message_id, direction, type, content, transcription)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, conversation_id, wa_message_id, direction, type, content, media_url, transcription, created_at`,
		convID, waID, direction, msgType, content, transcription,
	).Scan(&msg.ID, &msg.ConversationID, &msg.WAMessageID, &msg.Direction, &msg.Type,
		&msg.Content, &msg.MediaURL, &msg.Transcription, &msg.CreatedAt)
	return msg, err
}

func ListMessages(convID string) ([]*Message, error) {
	rows, err := postgres.DB.Query(
		`SELECT id, conversation_id, wa_message_id, direction, type, content, media_url, transcription, created_at
		 FROM messages WHERE conversation_id = $1 ORDER BY created_at ASC`,
		convID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		msg := &Message{}
		rows.Scan(&msg.ID, &msg.ConversationID, &msg.WAMessageID, &msg.Direction, &msg.Type,
			&msg.Content, &msg.MediaURL, &msg.Transcription, &msg.CreatedAt)
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
```

- [ ] **Step 4: Criar internal/crm/queue.go**

```go
package crm

import (
	"wapi/store/postgres"
)

type Queue struct {
	ID    string
	Name  string
	Color string
}

func ListQueues() ([]*Queue, error) {
	rows, err := postgres.DB.Query(`SELECT id, name, color FROM queues ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queues []*Queue
	for rows.Next() {
		q := &Queue{}
		rows.Scan(&q.ID, &q.Name, &q.Color)
		queues = append(queues, q)
	}
	return queues, nil
}

func CreateQueue(name, color string) (*Queue, error) {
	q := &Queue{}
	err := postgres.DB.QueryRow(
		`INSERT INTO queues (name, color) VALUES ($1, $2) RETURNING id, name, color`,
		name, color,
	).Scan(&q.ID, &q.Name, &q.Color)
	return q, err
}
```

- [ ] **Step 5: Compilar**

```bash
docker exec wapi-dev sh -c "cd /app && go build ./... 2>&1"
```

Esperado: sem erros.

- [ ] **Step 6: Commit**

```bash
cd /opt/wapi-refactor
git add internal/crm/
git commit -m "feat: add CRM package (contact, conversation, message, queue)"
```

---

## Task 3: Integrar CRM ao processamento de mensagens

**Files:**
- Create: `internal/crm/handler.go`
- Modify: `internal/instance/manager.go`

- [ ] **Step 1: Criar internal/crm/handler.go — lógica de entrada de mensagem**

```go
package crm

import "log"

type IncomingMessage struct {
	InstanceID    string
	WaMessageID   string
	SenderPhone   string
	SenderName    string
	Type          string
	Content       string
	Transcription string
}

// HandleIncomingMessage cria/atualiza contato e conversa, salva mensagem.
// Retorna (conversationID, contactName) para broadcast.
func HandleIncomingMessage(msg IncomingMessage) (string, string, error) {
	contact, err := FindOrCreateContact(msg.SenderPhone, msg.SenderName)
	if err != nil {
		log.Printf("[CRM] erro ao criar contato %s: %v", msg.SenderPhone, err)
		return "", "", err
	}

	conv, err := FindOrCreateConversation(msg.InstanceID, contact.ID)
	if err != nil {
		log.Printf("[CRM] erro ao criar conversa: %v", err)
		return "", "", err
	}

	content := msg.Content
	if content == "" && msg.Transcription != "" {
		content = "[áudio] " + msg.Transcription
	} else if content == "" {
		content = "[" + msg.Type + "]"
	}

	_, err = SaveMessage(conv.ID, msg.WaMessageID, "in", msg.Type, content, msg.Transcription)
	if err != nil {
		log.Printf("[CRM] erro ao salvar mensagem: %v", err)
		return "", "", err
	}

	UpdateConversationLastMessage(conv.ID, content)
	return conv.ID, contact.Name, nil
}
```

- [ ] **Step 2: Modificar internal/instance/manager.go — chamar CRM após processar mensagem**

Localizar a função `processMessage` (linha ~268) e adicionar chamada ao CRM após construir `msgData`. Substituir o bloco que termina com `inst.broadcastMessage(msgData)` para incluir a integração:

```go
// No final de processMessage, antes do return, substituir:
//   inst.broadcastMessage(msgData)
//   go inst.sendWebhook(msgData)
// Por:
	inst.handleCRMAndBroadcast(msgData, v.Info.ID, senderNumber, v.Info.PushName)
```

E adicionar o novo método na mesma classe:

```go
func (inst *Instance) handleCRMAndBroadcast(msgData map[string]interface{}, waID, senderPhone, senderName string) {
	content, _ := msgData["message"].(string)
	transcription, _ := msgData["transcription"].(string)
	msgType, _ := msgData["type"].(string)

	go func() {
		incoming := crm.IncomingMessage{
			InstanceID:    inst.ID,
			WaMessageID:   waID,
			SenderPhone:   senderPhone,
			SenderName:    senderName,
			Type:          msgType,
			Content:       content,
			Transcription: transcription,
		}
		crm.HandleIncomingMessage(incoming)
	}()

	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)
}
```

Adicionar import `"wapi/internal/crm"` no bloco de imports do arquivo.

- [ ] **Step 3: Atualizar processAudio para usar handleCRMAndBroadcast**

Localizar `processAudio` e substituir o final:

```go
// Antes (duas linhas no final de processAudio):
//   inst.broadcastMessage(msgData)
//   go inst.sendWebhook(msgData)
// Depois:
	if inst.TranscriptionEnabled {
		text, _ := transcriber.Transcribe(audioData, "audio.ogg")
		msgData["transcription"] = text
	}
	inst.handleCRMAndBroadcast(msgData, v.Info.ID, senderNumber, senderName)
```

Nota: `processAudio` não recebe `senderNumber` e `senderName` — precisar passar esses campos. Alterar assinatura de `processAudio`:

```go
// Antes:
func (inst *Instance) processAudio(v *events.Message, msgData map[string]interface{}) {
// Depois:
func (inst *Instance) processAudio(v *events.Message, msgData map[string]interface{}, senderNumber, senderName string) {
```

E na chamada em `processMessage`:

```go
// Antes:
go inst.processAudio(v, msgData)
// Depois:
go inst.processAudio(v, msgData, senderNumber, v.Info.PushName)
```

- [ ] **Step 4: Compilar**

```bash
docker exec wapi-dev sh -c "cd /app && go build ./... 2>&1"
```

Esperado: sem erros.

- [ ] **Step 5: Commit**

```bash
cd /opt/wapi-refactor
git add internal/crm/handler.go internal/instance/manager.go
git commit -m "feat: integrate CRM message processing into WhatsApp event handler"
```

---

## Task 4: API REST do CRM

**Files:**
- Create: `internal/handler/crm.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Criar internal/handler/crm.go**

```go
package handler

import (
	"net/http"
	"wapi/internal/crm"
	"wapi/internal/instance"

	"github.com/gin-gonic/gin"
)

// GET /api/queues
func ListQueues(c *gin.Context) {
	queues, err := crm.ListQueues()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, queues)
}

// POST /api/queues
func CreateQueue(c *gin.Context) {
	var req struct {
		Name  string `json:"name" binding:"required"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome é obrigatório"})
		return
	}
	if req.Color == "" {
		req.Color = "#2196F3"
	}
	q, err := crm.CreateQueue(req.Name, req.Color)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, q)
}

// GET /api/instances/:name/conversations
func ListConversations(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}
	convs, err := crm.ListConversations(inst.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if convs == nil {
		convs = []*crm.Conversation{}
	}
	c.JSON(http.StatusOK, convs)
}

// GET /api/conversations/:id/messages
func ListMessages(c *gin.Context) {
	convID := c.Param("id")
	msgs, err := crm.ListMessages(convID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []*crm.Message{}
	}
	c.JSON(http.StatusOK, msgs)
}
```

- [ ] **Step 2: Registrar rotas do CRM em cmd/server/main.go**

Adicionar no bloco de rotas, após o grupo `instances`:

```go
// CRM API
api := r.Group("/api", handler.AuthMiddleware())
{
    api.GET("/queues", handler.ListQueues)
    api.POST("/queues", handler.CreateQueue)
    api.GET("/instances/:name/conversations", handler.ListConversations)
    api.GET("/conversations/:id/messages", handler.ListMessages)
}
```

- [ ] **Step 3: Compilar e testar manualmente**

```bash
docker exec wapi-dev sh -c "cd /app && go build -o server ./cmd/server/ && pkill server; export \$(cat .env | grep -v '^#' | xargs) && ./server &"
sleep 3

# Obter token
TOKEN=$(curl -s -X POST http://localhost:8090/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

# Listar filas
curl -s http://localhost:8090/api/queues -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
```

Esperado: `[{"ID":"...","Name":"Atendimento","Color":"#4CAF50"}]`

- [ ] **Step 4: Commit**

```bash
cd /opt/wapi-refactor
git add internal/handler/crm.go cmd/server/main.go
git commit -m "feat: add CRM REST API (queues, conversations, messages)"
```

---

## Task 5: Frontend — Layout Base e Tela de Atendimento

**Files:**
- Create: `web/templates/layout.html`
- Create: `web/templates/atendimento.html`
- Create: `web/static/style.css`
- Create: `internal/handler/frontend.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Criar web/static/style.css**

```css
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #f0f2f5; height: 100vh; overflow: hidden; }

/* Layout */
.app { display: flex; height: 100vh; }
.sidebar { width: 380px; background: #fff; border-right: 1px solid #e0e0e0; display: flex; flex-direction: column; }
.main { flex: 1; display: flex; flex-direction: column; }

/* Header */
.header { background: #075e54; color: #fff; padding: 12px 16px; display: flex; align-items: center; gap: 12px; }
.header h1 { font-size: 1.1rem; font-weight: 600; }

/* Sidebar header */
.sidebar-header { background: #075e54; color: #fff; padding: 12px 16px; display: flex; align-items: center; justify-content: space-between; }
.sidebar-header h2 { font-size: 1rem; font-weight: 600; }

/* Instância selector */
.instance-select { background: rgba(255,255,255,0.2); color: #fff; border: none; border-radius: 4px; padding: 4px 8px; font-size: 0.85rem; cursor: pointer; }
.instance-select option { background: #075e54; }

/* Conversations list */
.conversations { flex: 1; overflow-y: auto; }
.conv-item { display: flex; align-items: center; padding: 12px 16px; border-bottom: 1px solid #f0f0f0; cursor: pointer; transition: background 0.15s; }
.conv-item:hover, .conv-item.active { background: #f5f5f5; }
.conv-avatar { width: 46px; height: 46px; border-radius: 50%; background: #25d366; color: #fff; display: flex; align-items: center; justify-content: center; font-weight: 700; font-size: 1.1rem; flex-shrink: 0; }
.conv-info { flex: 1; min-width: 0; margin-left: 12px; }
.conv-name { font-weight: 600; font-size: 0.9rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.conv-last { color: #666; font-size: 0.8rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; margin-top: 2px; }
.conv-meta { display: flex; flex-direction: column; align-items: flex-end; gap: 4px; }
.conv-time { font-size: 0.72rem; color: #999; }
.conv-badge { background: #25d366; color: #fff; border-radius: 50%; width: 20px; height: 20px; display: flex; align-items: center; justify-content: center; font-size: 0.7rem; font-weight: 700; }

/* Chat area */
.chat-placeholder { flex: 1; display: flex; align-items: center; justify-content: center; color: #999; flex-direction: column; gap: 8px; }
.chat-placeholder span { font-size: 3rem; }

.chat-area { flex: 1; display: flex; flex-direction: column; }
.chat-header { background: #fff; border-bottom: 1px solid #e0e0e0; padding: 12px 16px; display: flex; align-items: center; gap: 12px; }
.chat-header .avatar { width: 40px; height: 40px; border-radius: 50%; background: #25d366; color: #fff; display: flex; align-items: center; justify-content: center; font-weight: 700; }
.chat-header .info .name { font-weight: 600; }
.chat-header .info .phone { font-size: 0.8rem; color: #666; }

.messages { flex: 1; overflow-y: auto; padding: 16px; background: #e5ddd5; display: flex; flex-direction: column; gap: 4px; }
.msg { max-width: 65%; padding: 8px 12px; border-radius: 8px; font-size: 0.9rem; line-height: 1.4; position: relative; }
.msg-in { background: #fff; align-self: flex-start; border-radius: 0 8px 8px 8px; }
.msg-out { background: #dcf8c6; align-self: flex-end; border-radius: 8px 0 8px 8px; }
.msg-time { font-size: 0.7rem; color: #999; text-align: right; margin-top: 2px; }

.chat-input { background: #f0f0f0; padding: 12px 16px; display: flex; gap: 8px; align-items: center; }
.chat-input input { flex: 1; border: none; border-radius: 20px; padding: 10px 16px; font-size: 0.9rem; outline: none; background: #fff; }
.chat-input button { background: #25d366; color: #fff; border: none; border-radius: 50%; width: 40px; height: 40px; cursor: pointer; font-size: 1.1rem; display: flex; align-items: center; justify-content: center; }
.chat-input button:hover { background: #128c7e; }

/* Status badge */
.status-dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
.status-dot.connected { background: #25d366; }
.status-dot.disconnected { background: #f44336; }

/* Empty state */
.empty { padding: 32px 16px; text-align: center; color: #999; font-size: 0.9rem; }
```

- [ ] **Step 2: Criar web/templates/layout.html**

```html
<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} — CRM WhatsApp</title>
  <link rel="stylesheet" href="/static/style.css">
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
</head>
<body>
  {{template "content" .}}
</body>
</html>
```

- [ ] **Step 3: Criar web/templates/atendimento.html**

```html
{{define "content"}}
<div class="app" x-data="atendimento()" x-init="init()">

  <!-- Sidebar: lista de conversas -->
  <div class="sidebar">
    <div class="sidebar-header">
      <h2>Atendimento</h2>
      <select class="instance-select" x-model="selectedInstance" @change="loadConversations()">
        <template x-for="inst in instances" :key="inst.id">
          <option :value="inst.name" x-text="inst.name + (inst.status === 'connected' ? ' ●' : ' ○')"></option>
        </template>
      </select>
    </div>

    <div class="conversations">
      <template x-if="conversations.length === 0">
        <div class="empty">Nenhuma conversa ainda</div>
      </template>
      <template x-for="conv in conversations" :key="conv.ID">
        <div class="conv-item" :class="{active: activeConv && activeConv.ID === conv.ID}" @click="openConversation(conv)">
          <div class="conv-avatar" x-text="initials(conv.ContactName || conv.ContactPhone)"></div>
          <div class="conv-info">
            <div class="conv-name" x-text="conv.ContactName || conv.ContactPhone"></div>
            <div class="conv-last" x-text="conv.LastMessage || 'Sem mensagens'"></div>
          </div>
          <div class="conv-meta">
            <div class="conv-time" x-text="formatTime(conv.LastMessageAt)"></div>
            <template x-if="conv.UnreadCount > 0">
              <div class="conv-badge" x-text="conv.UnreadCount"></div>
            </template>
          </div>
        </div>
      </template>
    </div>
  </div>

  <!-- Área principal -->
  <div class="main">
    <!-- Placeholder quando nenhuma conversa selecionada -->
    <template x-if="!activeConv">
      <div class="chat-placeholder">
        <span>💬</span>
        <p>Selecione uma conversa para atender</p>
      </div>
    </template>

    <!-- Chat ativo -->
    <template x-if="activeConv">
      <div class="chat-area">
        <div class="chat-header">
          <div class="avatar" x-text="initials(activeConv.ContactName || activeConv.ContactPhone)"></div>
          <div class="info">
            <div class="name" x-text="activeConv.ContactName || activeConv.ContactPhone"></div>
            <div class="phone" x-text="activeConv.ContactPhone"></div>
          </div>
        </div>

        <div class="messages" id="messages-container">
          <template x-for="msg in messages" :key="msg.ID">
            <div class="msg" :class="msg.Direction === 'in' ? 'msg-in' : 'msg-out'">
              <div x-text="msg.Content"></div>
              <template x-if="msg.Transcription">
                <div style="font-style:italic;color:#666;font-size:0.8rem;margin-top:4px" x-text="'🎙 ' + msg.Transcription"></div>
              </template>
              <div class="msg-time" x-text="formatTime(msg.CreatedAt)"></div>
            </div>
          </template>
        </div>

        <div class="chat-input">
          <input type="text" placeholder="Digite uma mensagem..." x-model="newMessage"
                 @keyup.enter="sendMessage()">
          <button @click="sendMessage()">➤</button>
        </div>
      </div>
    </template>
  </div>
</div>

<script>
function atendimento() {
  return {
    instances: [],
    selectedInstance: '',
    conversations: [],
    activeConv: null,
    messages: [],
    newMessage: '',
    token: localStorage.getItem('token') || '',
    sseSource: null,

    async init() {
      if (!this.token) {
        window.location.href = '/login';
        return;
      }
      await this.loadInstances();
      if (this.instances.length > 0) {
        this.selectedInstance = this.instances[0].name;
        await this.loadConversations();
        this.connectSSE();
      }
    },

    async loadInstances() {
      const r = await fetch('/instances', { headers: { Authorization: 'Bearer ' + this.token } });
      if (r.status === 401) { window.location.href = '/login'; return; }
      this.instances = await r.json();
    },

    async loadConversations() {
      if (!this.selectedInstance) return;
      const r = await fetch('/api/instances/' + this.selectedInstance + '/conversations',
        { headers: { Authorization: 'Bearer ' + this.token } });
      this.conversations = await r.json() || [];
    },

    async openConversation(conv) {
      this.activeConv = conv;
      conv.UnreadCount = 0;
      const r = await fetch('/api/conversations/' + conv.ID + '/messages',
        { headers: { Authorization: 'Bearer ' + this.token } });
      this.messages = await r.json() || [];
      this.$nextTick(() => {
        const el = document.getElementById('messages-container');
        if (el) el.scrollTop = el.scrollHeight;
      });
    },

    async sendMessage() {
      if (!this.newMessage.trim() || !this.activeConv) return;
      const text = this.newMessage;
      this.newMessage = '';
      await fetch('/instances/' + this.selectedInstance + '/send/text', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', apikey: this.activeConv.APIKey || '' },
        body: JSON.stringify({ number: this.activeConv.ContactPhone, message: text })
      });
    },

    connectSSE() {
      if (this.sseSource) this.sseSource.close();
      if (!this.selectedInstance) return;
      this.sseSource = new EventSource('/instances/' + this.selectedInstance + '/sse');
      this.sseSource.onmessage = (e) => {
        try {
          const data = JSON.parse(e.data);
          if (data.event === 'message') {
            this.loadConversations();
            if (this.activeConv) {
              this.messages.push({
                ID: Date.now().toString(),
                Direction: 'in',
                Content: data.data.message,
                Transcription: data.data.transcription || '',
                CreatedAt: data.data.timestamp
              });
              this.$nextTick(() => {
                const el = document.getElementById('messages-container');
                if (el) el.scrollTop = el.scrollHeight;
              });
            }
          }
        } catch(err) {}
      };
    },

    initials(name) {
      if (!name) return '?';
      return name.split(' ').slice(0,2).map(w => w[0]).join('').toUpperCase();
    },

    formatTime(ts) {
      if (!ts) return '';
      const d = new Date(ts);
      const now = new Date();
      if (d.toDateString() === now.toDateString()) {
        return d.toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
      }
      return d.toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit' });
    }
  }
}
</script>
{{end}}
```

- [ ] **Step 4: Criar internal/handler/frontend.go**

```go
package handler

import (
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

var tmpl *template.Template

func init() {
	var err error
	tmpl, err = template.ParseGlob(filepath.Join("web", "templates", "*.html"))
	if err != nil {
		// Em desenvolvimento, o erro é impresso mas não fatal
		// (pode ocorrer se os templates ainda não existem no startup)
		_ = err
	}
}

func renderTemplate(c *gin.Context, name string, data gin.H) {
	if tmpl == nil {
		var err error
		tmpl, err = template.ParseGlob(filepath.Join("web", "templates", "*.html"))
		if err != nil {
			c.String(http.StatusInternalServerError, "Erro ao carregar templates: %v", err)
			return
		}
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "layout.html", data); err != nil {
		c.String(http.StatusInternalServerError, "Erro ao renderizar template: %v", err)
	}
}

func AtendimentoPage(c *gin.Context) {
	renderTemplate(c, "layout.html", gin.H{
		"Title": "Atendimento",
	})
}

func LoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", nil)
}
```

- [ ] **Step 5: Atualizar cmd/server/main.go — registrar rotas de frontend e arquivos estáticos**

Substituir as linhas:
```go
r.StaticFile("/", "./web/index.html")
r.Static("/web", "./web")
```

Por:
```go
r.Static("/static", "./web/static")
r.GET("/", handler.AtendimentoPage)
r.GET("/login", handler.LoginPage)
```

- [ ] **Step 6: Compilar**

```bash
docker exec wapi-dev sh -c "cd /app && go build -o server ./cmd/server/ 2>&1"
```

Esperado: sem erros.

- [ ] **Step 7: Reiniciar e testar no browser**

```bash
docker exec wapi-dev sh -c "pkill server; cd /app && export \$(cat .env | grep -v '^#' | xargs) && ./server > /tmp/wapi.log 2>&1 &"
sleep 2
cat /tmp/wapi.log
```

Abrir `http://65.21.109.105:8090` no browser — deve mostrar a tela de atendimento (vai redirecionar para login se sem token).

- [ ] **Step 8: Criar template de login básico (web/templates/login.html)**

```html
{{define "login-content"}}
<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="UTF-8">
  <title>Login — CRM WhatsApp</title>
  <style>
    body { font-family: -apple-system, sans-serif; background: #075e54; min-height: 100vh; display: flex; align-items: center; justify-content: center; }
    .card { background: #fff; border-radius: 12px; padding: 40px; width: 360px; box-shadow: 0 4px 20px rgba(0,0,0,0.15); }
    h1 { color: #075e54; margin-bottom: 24px; font-size: 1.4rem; text-align: center; }
    input { width: 100%; border: 1px solid #ddd; border-radius: 6px; padding: 10px 14px; font-size: 0.95rem; margin-bottom: 12px; outline: none; }
    input:focus { border-color: #25d366; }
    button { width: 100%; background: #25d366; color: #fff; border: none; border-radius: 6px; padding: 12px; font-size: 1rem; cursor: pointer; font-weight: 600; }
    button:hover { background: #128c7e; }
    .error { color: #f44336; font-size: 0.85rem; margin-bottom: 8px; text-align: center; }
  </style>
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
</head>
<body>
  <div class="card" x-data="login()">
    <h1>💬 CRM WhatsApp</h1>
    <div class="error" x-text="error" x-show="error"></div>
    <input type="text" placeholder="Usuário" x-model="username" @keyup.enter="doLogin()">
    <input type="password" placeholder="Senha" x-model="password" @keyup.enter="doLogin()">
    <button @click="doLogin()" :disabled="loading" x-text="loading ? 'Entrando...' : 'Entrar'"></button>
  </div>
  <script>
  function login() {
    return {
      username: '', password: '', error: '', loading: false,
      async doLogin() {
        this.loading = true; this.error = '';
        const r = await fetch('/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: this.username, password: this.password })
        });
        const data = await r.json();
        if (r.ok) {
          localStorage.setItem('token', data.token);
          window.location.href = '/';
        } else {
          this.error = data.error || 'Credenciais inválidas';
        }
        this.loading = false;
      }
    }
  }
  </script>
</body>
</html>
{{end}}
```

Atualizar `frontend.go` para renderizar login corretamente:

```go
func LoginPage(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	tmpl.ExecuteTemplate(c.Writer, "login-content", nil)
}
```

- [ ] **Step 9: Commit final da Task 5**

```bash
cd /opt/wapi-refactor
git add web/templates/ web/static/ internal/handler/frontend.go cmd/server/main.go
git commit -m "feat: add attendance frontend with Alpine.js (conversations list, chat view, login)"
```

---

## Task 6: Envio de mensagens pela tela de atendimento

**Files:**
- Modify: `internal/crm/message.go`
- Modify: `internal/handler/crm.go`

- [ ] **Step 1: Adicionar endpoint de envio via CRM em handler/crm.go**

O frontend já usa `POST /instances/:name/send/text` com `apikey`. Para o CRM funcionar sem expor a apikey no frontend, adicionar endpoint autenticado por JWT que envia via instância:

```go
// POST /api/instances/:name/send — autenticado por JWT, salva no CRM
func SendMessageCRM(c *gin.Context) {
	name := c.Param("name")
	inst, ok := instance.Global.GetByName(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "instância não encontrada"})
		return
	}

	var req struct {
		ConversationID string `json:"conversation_id" binding:"required"`
		Phone          string `json:"phone" binding:"required"`
		Message        string `json:"message" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Enviar via whatsmeow
	if err := inst.SendText(req.Phone, req.Message); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Salvar no CRM como mensagem de saída
	crm.SaveMessage(req.ConversationID, "", "out", "text", req.Message, "")

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
```

- [ ] **Step 2: Adicionar método SendText à instância**

Em `internal/instance/manager.go`, adicionar:

```go
func (inst *Instance) SendText(phone, text string) error {
	if inst.WAClient == nil || !inst.WAClient.IsConnected() {
		return fmt.Errorf("instância não conectada")
	}
	jid, err := types.ParseJID(phone + "@s.whatsapp.net")
	if err != nil {
		return fmt.Errorf("número inválido: %w", err)
	}
	_, err = inst.WAClient.SendMessage(inst.ctx, jid, inst.WAClient.BuildTextMessage(text))
	return err
}
```

- [ ] **Step 3: Registrar rota em main.go**

No grupo `/api`:
```go
api.POST("/instances/:name/send", handler.SendMessageCRM)
```

- [ ] **Step 4: Atualizar o sendMessage() no frontend (atendimento.html)**

Substituir a função `sendMessage` no JavaScript:

```javascript
async sendMessage() {
  if (!this.newMessage.trim() || !this.activeConv) return;
  const text = this.newMessage;
  this.newMessage = '';
  const r = await fetch('/api/instances/' + this.selectedInstance + '/send', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + this.token },
    body: JSON.stringify({
      conversation_id: this.activeConv.ID,
      phone: this.activeConv.ContactPhone,
      message: text
    })
  });
  if (r.ok) {
    this.messages.push({
      ID: Date.now().toString(),
      Direction: 'out',
      Content: text,
      Transcription: '',
      CreatedAt: new Date().toISOString()
    });
    this.$nextTick(() => {
      const el = document.getElementById('messages-container');
      if (el) el.scrollTop = el.scrollHeight;
    });
  }
},
```

- [ ] **Step 5: Compilar e testar**

```bash
docker exec wapi-dev sh -c "cd /app && go build -o server ./cmd/server/ 2>&1"
```

Esperado: sem erros.

- [ ] **Step 6: Commit**

```bash
cd /opt/wapi-refactor
git add internal/handler/crm.go internal/instance/manager.go web/templates/atendimento.html cmd/server/main.go
git commit -m "feat: add authenticated send endpoint and wire up chat input in frontend"
```

---

## Resumo do que esta Parte 1 entrega

Ao final das 6 tasks:

✅ **Banco de dados** com tabelas de CRM (filas, contatos, conversas, mensagens)  
✅ **Processamento automático** de mensagens recebidas → cria contato + conversa + salva mensagem  
✅ **API REST** para listar filas, conversas e mensagens  
✅ **Tela de atendimento** com lista de conversas em tempo real (SSE) e chat  
✅ **Login** com JWT persistido em localStorage  
✅ **Envio de mensagens** pela tela, salvo no histórico  

**O que NÃO está nesta parte (para as próximas):**
- Atribuição de agente/fila manual
- Múltiplos atendentes com controle de acesso
- Transferência de conversa
- Mensagens de mídia na tela
- Notificações sonoras
