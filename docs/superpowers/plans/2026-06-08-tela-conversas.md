# Tela de Conversas em Tempo Real

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a real-time two-pane conversations screen backed by persistent messages storage and WebSocket push.

**Architecture:** New DB tables (`contacts`, `messages`) store all incoming/outgoing messages. A central WebSocket hub broadcasts new message events to all connected browsers. The frontend is a Go html/template page with Alpine.js: left panel lists conversations (ordered by last message), right panel shows the thread and a reply input. WebSocket reconnects automatically. Sending messages calls the existing `POST /instances/:name/send/text` endpoint; the send handler now also saves to DB and broadcasts. Group messages (server=`g.us`) are skipped — only 1:1 chats.

**Tech Stack:** Go + Gin + `github.com/coder/websocket` (already in go.mod), PostgreSQL, Alpine.js v3 (CDN), Tailwind CSS (CDN)

---

## File Map

| File | Ação | Responsabilidade |
|------|------|-----------------|
| `store/postgres/store.go` | Modificar | Adicionar tabelas contacts + messages na migration |
| `internal/hub/hub.go` | Criar | WebSocket hub global: Register, Unregister, Broadcast |
| `internal/handler/ws.go` | Criar | GET /ws — upgrade HTTP → WebSocket, JWT via query param |
| `internal/handler/conversations.go` | Criar | GET /api/conversations, GET /api/conversations/:name/:phone/messages |
| `internal/instance/manager.go` | Modificar | processMessage: salvar no DB + broadcast via hub (1:1 apenas) |
| `internal/handler/send.go` | Modificar | SendText: salvar mensagem enviada no DB + broadcast via hub |
| `web/templates/layout.html` | Modificar | Adicionar link "Conversas" no nav |
| `web/templates/conversations.html` | Criar | Página de conversas: dois painéis, WebSocket, envio |
| `cmd/server/main.go` | Modificar | Registrar rotas /ws, /api/conversations, GET /conversations |

---

## Task 1: DB migration — contacts + messages

**Files:**
- Modify: `store/postgres/store.go` (função `Migrate`)

- [ ] **Step 1: Adicionar tabelas na função Migrate**

Em `store/postgres/store.go`, na string `query` dentro de `Migrate()`, **antes** do fechamento de backtick, adicionar:

```go
	CREATE TABLE IF NOT EXISTS contacts (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
		phone VARCHAR(50) NOT NULL,
		name VARCHAR(255) DEFAULT '',
		created_at TIMESTAMP DEFAULT NOW(),
		UNIQUE(instance_id, phone)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
		contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
		direction VARCHAR(3) NOT NULL CHECK (direction IN ('in', 'out')),
		content TEXT NOT NULL DEFAULT '',
		type VARCHAR(20) NOT NULL DEFAULT 'text',
		wa_message_id VARCHAR(255) DEFAULT '',
		created_at TIMESTAMP DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_messages_conv ON messages (contact_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_instance ON messages (instance_id, created_at DESC);
```

- [ ] **Step 2: Compilar e reiniciar para aplicar a migration**

```bash
docker exec -d wapi-dev sh -c "cd /app && go build -o server ./cmd/server && pkill server 2>/dev/null; sleep 1; ./server >> /tmp/wapi.log 2>&1"
sleep 4
```

- [ ] **Step 3: Verificar tabelas no banco**

```bash
docker exec vendassit-dev-postgres psql -U wapi -d wapi -c "\dt"
```

Esperado: lista incluindo `contacts` e `messages`.

- [ ] **Step 4: Commit**

```bash
cd /opt/wapi-refactor
git add store/postgres/store.go
git commit -m "feat: add contacts and messages tables to migration"
```

---

## Task 2: WebSocket hub + endpoint

**Files:**
- Create: `internal/hub/hub.go`
- Create: `internal/handler/ws.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Criar hub global**

Criar `internal/hub/hub.go`:

```go
package hub

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (cl *Client) Send(msg []byte) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl.conn.Write(ctx, websocket.MessageText, msg) //nolint
}

type Hub struct {
	clients map[*Client]struct{}
	mu      sync.RWMutex
}

var Global = &Hub{clients: make(map[*Client]struct{})}

func (h *Hub) Register(conn *websocket.Conn) *Client {
	cl := &Client{conn: conn}
	h.mu.Lock()
	h.clients[cl] = struct{}{}
	h.mu.Unlock()
	return cl
}

func (h *Hub) Unregister(cl *Client) {
	h.mu.Lock()
	delete(h.clients, cl)
	h.mu.Unlock()
}

func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	cls := make([]*Client, 0, len(h.clients))
	for cl := range h.clients {
		cls = append(cls, cl)
	}
	h.mu.RUnlock()
	for _, cl := range cls {
		go cl.Send(msg)
	}
}
```

- [ ] **Step 2: Criar handler WebSocket**

Criar `internal/handler/ws.go`:

```go
package handler

import (
	"context"
	"wapi/internal/auth"
	"wapi/internal/hub"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func WSHandler(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.String(401, "token obrigatório")
		return
	}
	if _, err := auth.ValidateToken(token); err != nil {
		c.String(401, "token inválido")
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	cl := hub.Global.Register(conn)
	defer hub.Global.Unregister(cl)

	// Mantém a conexão aberta; descarta mensagens do cliente
	for {
		_, _, err := conn.Read(context.Background())
		if err != nil {
			return
		}
	}
}
```

- [ ] **Step 3: Registrar rota /ws em main.go**

Em `cmd/server/main.go`, no bloco de rotas SSE/QR Code (sem autenticação), adicionar:

```go
r.GET("/ws", handler.WSHandler)
```

- [ ] **Step 4: Compilar**

```bash
docker exec wapi-dev sh -c "cd /app && go build ./..." 2>&1
```

Esperado: sem erros.

- [ ] **Step 5: Testar WebSocket com wscat (ou websocat)**

```bash
# Na máquina host, obter token primeiro:
TOKEN=$(curl -s -X POST http://localhost:8090/login \
  -d "username=admin&password=admin123" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -D - | grep "Set-Cookie" | grep -oP 'wapi_token=\K[^;]+')

# Conectar via WebSocket (instalar websocat: cargo install websocat):
echo "Token: $TOKEN"
# Testar que a conexão é estabelecida:
curl -s -i -N -H "Upgrade: websocket" -H "Connection: Upgrade" \
  -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Sec-WebSocket-Version: 13" \
  "http://localhost:8090/ws?token=$TOKEN" | head -5
```

Esperado: resposta HTTP 101 Switching Protocols.

- [ ] **Step 6: Commit**

```bash
cd /opt/wapi-refactor
git add internal/hub/hub.go internal/handler/ws.go cmd/server/main.go
git commit -m "feat: add WebSocket hub and /ws endpoint with JWT auth"
```

---

## Task 3: Salvar mensagens recebidas no DB + broadcast

**Files:**
- Modify: `internal/instance/manager.go` (funções `processMessage` e `broadcastMessage`)

- [ ] **Step 1: Adicionar função de persistência de mensagem no manager.go**

Em `internal/instance/manager.go`, adicionar os imports `wapi/internal/hub` e `wapi/store/postgres` (já estão), `encoding/json` (já está), `github.com/google/uuid` e a seguinte função após `broadcastMessage`:

Adicionar imports necessários no topo do arquivo (se não existirem):
```go
"github.com/google/uuid"
"wapi/internal/hub"
```

Adicionar função `saveMessage` no final do arquivo, antes do `Ctx()`:

```go
// saveMessage salva mensagem no banco e faz broadcast via WebSocket.
// Retorna silenciosamente em caso de erro para não interromper o fluxo.
func (inst *Instance) saveMessage(phone, pushName, content, msgType, waID, direction string) {
	// Pular mensagens de grupo
	if direction == "" {
		return
	}

	// Upsert contact
	var contactID string
	err := postgres.DB.QueryRow(
		`INSERT INTO contacts (instance_id, phone, name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (instance_id, phone) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`,
		inst.ID, phone, pushName,
	).Scan(&contactID)
	if err != nil {
		log.Printf("[DB] Erro ao upsert contact: %v", err)
		return
	}

	// Insert message
	msgID := uuid.New().String()
	var createdAt string
	err = postgres.DB.QueryRow(
		`INSERT INTO messages (id, instance_id, contact_id, direction, content, type, wa_message_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING created_at`,
		msgID, inst.ID, contactID, direction, content, msgType, waID,
	).Scan(&createdAt)
	if err != nil {
		log.Printf("[DB] Erro ao salvar mensagem: %v", err)
		return
	}

	// Broadcast via WebSocket
	payload := map[string]interface{}{
		"event": "new_message",
		"data": map[string]interface{}{
			"id":            msgID,
			"instance_id":   inst.ID,
			"instance_name": inst.Name,
			"contact_id":    contactID,
			"phone":         phone,
			"name":          pushName,
			"direction":     direction,
			"content":       content,
			"type":          msgType,
			"created_at":    createdAt,
		},
	}
	jsonBytes, _ := json.Marshal(payload)
	hub.Global.Broadcast(jsonBytes)
}
```

- [ ] **Step 2: Chamar saveMessage no processMessage**

No método `processMessage`, **após** o bloco que define `msgData["message"]` e **antes** do `inst.broadcastMessage(msgData)` no caso texto (e antes do `return` para áudio), adicionar a chamada:

Localizar o bloco:
```go
	if v.Message.GetConversation() != "" {
		msgData["message"] = v.Message.GetConversation()
	} else if v.Message.GetExtendedTextMessage() != nil {
		msgData["message"] = v.Message.GetExtendedTextMessage().GetText()
	} else if v.Message.GetImageMessage() != nil {
		msgData["message"] = "[imagem]"
		if v.Message.GetImageMessage().GetCaption() != "" {
			msgData["message"] = v.Message.GetImageMessage().GetCaption()
		}
	} else if v.Message.GetAudioMessage() != nil {
		msgData["message"] = "[áudio]"
		msgData["type"] = "audio"
		go inst.processAudio(v, msgData)
		return
	}

	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)
```

Substituir por:

```go
	content := ""
	msgTypeStr := "text"

	if v.Message.GetConversation() != "" {
		content = v.Message.GetConversation()
		msgData["message"] = content
	} else if v.Message.GetExtendedTextMessage() != nil {
		content = v.Message.GetExtendedTextMessage().GetText()
		msgData["message"] = content
	} else if v.Message.GetImageMessage() != nil {
		content = "[imagem]"
		if v.Message.GetImageMessage().GetCaption() != "" {
			content = v.Message.GetImageMessage().GetCaption()
		}
		msgData["message"] = content
		msgTypeStr = "image"
	} else if v.Message.GetAudioMessage() != nil {
		msgData["message"] = "[áudio]"
		msgData["type"] = "audio"
		go inst.processAudio(v, msgData)
		if !isGroup {
			go inst.saveMessage(senderNumber, v.Info.PushName, "[áudio]", "audio", v.Info.ID, "in")
		}
		return
	}

	if !isGroup {
		go inst.saveMessage(senderNumber, v.Info.PushName, content, msgTypeStr, v.Info.ID, "in")
	}

	inst.broadcastMessage(msgData)
	go inst.sendWebhook(msgData)
```

- [ ] **Step 3: Compilar**

```bash
docker exec wapi-dev sh -c "cd /app && go build ./..." 2>&1
```

Esperado: sem erros.

- [ ] **Step 4: Reiniciar e testar**

```bash
docker exec -d wapi-dev sh -c "cd /app && pkill server 2>/dev/null; sleep 1; ./server >> /tmp/wapi.log 2>&1"
sleep 3
```

Enviar uma mensagem WhatsApp para uma instância conectada. Verificar no banco:

```bash
docker exec vendassit-dev-postgres psql -U wapi -d wapi -c "SELECT phone, name FROM contacts LIMIT 5;"
docker exec vendassit-dev-postgres psql -U wapi -d wapi -c "SELECT direction, content, type FROM messages ORDER BY created_at DESC LIMIT 5;"
```

- [ ] **Step 5: Commit**

```bash
cd /opt/wapi-refactor
git add internal/instance/manager.go internal/hub/hub.go
git commit -m "feat: save incoming messages to DB and broadcast via WebSocket hub"
```

---

## Task 4: Salvar mensagens enviadas no DB + broadcast

**Files:**
- Modify: `internal/handler/send.go` (função `SendText`)

- [ ] **Step 1: Adicionar persistência no SendText**

Em `internal/handler/send.go`, adicionar imports:
```go
"wapi/internal/hub"
"wapi/store/postgres"
"github.com/google/uuid"
```

Localizar o trecho em `SendText` após o `service.SendText` bem-sucedido:
```go
	if err := service.SendText(inst, number, req.Message); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "mensagem enviada com sucesso"})
```

Substituir por:
```go
	if err := service.SendText(inst, number, req.Message); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Salvar mensagem enviada no DB e broadcast
	go func() {
		var contactID string
		err := postgres.DB.QueryRow(
			`INSERT INTO contacts (instance_id, phone, name)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (instance_id, phone) DO UPDATE SET name = EXCLUDED.name
			 RETURNING id`,
			inst.ID, number, number,
		).Scan(&contactID)
		if err != nil {
			log.Printf("[DB] Erro ao upsert contact (send): %v", err)
			return
		}
		msgID := uuid.New().String()
		var createdAt string
		err = postgres.DB.QueryRow(
			`INSERT INTO messages (id, instance_id, contact_id, direction, content, type)
			 VALUES ($1, $2, $3, 'out', $4, 'text')
			 RETURNING created_at`,
			msgID, inst.ID, contactID, req.Message,
		).Scan(&createdAt)
		if err != nil {
			log.Printf("[DB] Erro ao salvar mensagem enviada: %v", err)
			return
		}
		payload := map[string]interface{}{
			"event": "new_message",
			"data": map[string]interface{}{
				"id":            msgID,
				"instance_id":   inst.ID,
				"instance_name": inst.Name,
				"contact_id":    contactID,
				"phone":         number,
				"name":          number,
				"direction":     "out",
				"content":       req.Message,
				"type":          "text",
				"created_at":    createdAt,
			},
		}
		jsonBytes, _ := json.Marshal(payload)
		hub.Global.Broadcast(jsonBytes)
	}()

	c.JSON(http.StatusOK, gin.H{"message": "mensagem enviada com sucesso"})
```

- [ ] **Step 2: Adicionar imports necessários no send.go**

No bloco de imports de `send.go`, adicionar as novas dependências:
```go
"encoding/json"
"log"
"wapi/internal/hub"
"wapi/store/postgres"
"github.com/google/uuid"
```

(Vários já existem — adicionar apenas os que faltam.)

- [ ] **Step 3: Compilar**

```bash
docker exec wapi-dev sh -c "cd /app && go build ./..." 2>&1
```

Esperado: sem erros.

- [ ] **Step 4: Commit**

```bash
cd /opt/wapi-refactor
git add internal/handler/send.go
git commit -m "feat: persist outgoing text messages to DB and broadcast via WebSocket"
```

---

## Task 5: REST API de conversas

**Files:**
- Create: `internal/handler/conversations.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Criar handler de conversas**

Criar `internal/handler/conversations.go`:

```go
package handler

import (
	"net/http"
	"wapi/store/postgres"

	"github.com/gin-gonic/gin"
)

// ListConversations retorna a última mensagem de cada conversa, ordenada por recência.
func ListConversations(c *gin.Context) {
	rows, err := postgres.DB.Query(`
		SELECT
			c.id AS contact_id,
			c.phone,
			CASE WHEN c.name != '' THEN c.name ELSE c.phone END AS name,
			i.id AS instance_id,
			i.name AS instance_name,
			m.content AS last_message,
			m.direction AS last_direction,
			m.type AS last_type,
			m.created_at AS last_message_at
		FROM contacts c
		JOIN instances i ON i.id = c.instance_id
		LEFT JOIN LATERAL (
			SELECT content, direction, type, created_at
			FROM messages
			WHERE contact_id = c.id
			ORDER BY created_at DESC
			LIMIT 1
		) m ON true
		WHERE m.created_at IS NOT NULL
		ORDER BY m.created_at DESC
		LIMIT 50
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Conv struct {
		ContactID     string `json:"contact_id"`
		Phone         string `json:"phone"`
		Name          string `json:"name"`
		InstanceID    string `json:"instance_id"`
		InstanceName  string `json:"instance_name"`
		LastMessage   string `json:"last_message"`
		LastDirection string `json:"last_direction"`
		LastType      string `json:"last_type"`
		LastMessageAt string `json:"last_message_at"`
	}

	convs := make([]Conv, 0)
	for rows.Next() {
		var conv Conv
		if err := rows.Scan(
			&conv.ContactID, &conv.Phone, &conv.Name,
			&conv.InstanceID, &conv.InstanceName,
			&conv.LastMessage, &conv.LastDirection, &conv.LastType,
			&conv.LastMessageAt,
		); err != nil {
			continue
		}
		convs = append(convs, conv)
	}
	c.JSON(http.StatusOK, convs)
}

// GetMessages retorna as mensagens de uma conversa específica.
func GetMessages(c *gin.Context) {
	instanceName := c.Param("name")
	phone := c.Param("phone")

	rows, err := postgres.DB.Query(`
		SELECT m.id, m.direction, m.content, m.type, m.created_at
		FROM messages m
		JOIN contacts c ON c.id = m.contact_id
		JOIN instances i ON i.id = m.instance_id
		WHERE i.name = $1 AND c.phone = $2
		ORDER BY m.created_at ASC
		LIMIT 200
	`, instanceName, phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Msg struct {
		ID        string `json:"id"`
		Direction string `json:"direction"`
		Content   string `json:"content"`
		Type      string `json:"type"`
		CreatedAt string `json:"created_at"`
	}

	msgs := make([]Msg, 0)
	for rows.Next() {
		var msg Msg
		if err := rows.Scan(&msg.ID, &msg.Direction, &msg.Content, &msg.Type, &msg.CreatedAt); err != nil {
			continue
		}
		msgs = append(msgs, msg)
	}
	c.JSON(http.StatusOK, msgs)
}
```

- [ ] **Step 2: Registrar rotas em main.go**

Em `cmd/server/main.go`, no grupo `instances` (autenticado com JWT), adicionar após as rotas de instâncias:

```go
// API de conversas — JWT
apiGroup := r.Group("/api", handler.AuthMiddleware())
{
    apiGroup.GET("/conversations", handler.ListConversations)
    apiGroup.GET("/conversations/:name/:phone/messages", handler.GetMessages)
}
```

- [ ] **Step 3: Compilar e reiniciar**

```bash
docker exec -d wapi-dev sh -c "cd /app && go build -o server ./cmd/server && pkill server 2>/dev/null; sleep 1; ./server >> /tmp/wapi.log 2>&1"
sleep 4
```

- [ ] **Step 4: Testar API**

```bash
TOKEN=$(curl -s -X POST http://localhost:8090/login \
  -d "username=admin&password=admin123" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -D - | grep "Set-Cookie" | grep -oP 'wapi_token=\K[^;]+')

curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8090/api/conversations | python3 -m json.tool
```

Esperado: array JSON (vazio `[]` se não houver mensagens ainda, ou com conversas se já enviou/recebeu).

- [ ] **Step 5: Commit**

```bash
cd /opt/wapi-refactor
git add internal/handler/conversations.go cmd/server/main.go
git commit -m "feat: add /api/conversations and /api/conversations/:name/:phone/messages endpoints"
```

---

## Task 6: Frontend — página de conversas

**Files:**
- Modify: `web/templates/layout.html` (adicionar nav links)
- Create: `web/templates/conversations.html`
- Modify: `internal/handler/web.go` (adicionar WebConversations handler)
- Modify: `cmd/server/main.go` (rota GET /conversations)

- [ ] **Step 1: Atualizar layout.html — adicionar nav links**

Em `web/templates/layout.html`, substituir o conteúdo do `<nav>`:

```html
  <nav class="bg-white border-b border-gray-200 px-6 py-4">
    <div class="flex items-center justify-between max-w-6xl mx-auto">
      <div class="flex items-center gap-6">
        <span class="text-lg font-semibold text-gray-800">WAPI</span>
        {{ if .Token }}
        <a href="/connections" class="text-sm text-gray-600 hover:text-gray-900 transition-colors">Conexões</a>
        <a href="/conversations" class="text-sm text-gray-600 hover:text-gray-900 transition-colors">Conversas</a>
        {{ end }}
      </div>
      {{ if .Token }}
      <a href="/logout" class="text-sm text-gray-500 hover:text-gray-800 transition-colors">Sair</a>
      {{ end }}
    </div>
  </nav>
```

- [ ] **Step 2: Adicionar WebConversations handler em web.go**

Em `internal/handler/web.go`, adicionar a função:

```go
func WebConversations(c *gin.Context) {
	token, _ := c.Get("token")
	render(c, http.StatusOK, "conversations.html", gin.H{
		"Token": token,
	})
}
```

- [ ] **Step 3: Registrar rota GET /conversations em main.go**

No bloco `webGroup` em `cmd/server/main.go`, adicionar:

```go
webGroup.GET("/conversations", handler.WebConversations)
```

- [ ] **Step 4: Criar conversations.html**

Criar `web/templates/conversations.html`:

```html
{{ template "layout.html" . }}
{{ define "title" }}Conversas — WAPI{{ end }}
{{ define "content" }}

<script>
const TOKEN = document.querySelector('meta[name="auth-token"]')?.content || '';

function conversationsPage() {
  return {
    conversations: [],
    activeConv: null,
    messages: [],
    messageInput: '',
    sending: false,
    ws: null,

    init() {
      this.loadConversations();
      this.connectWS();
    },

    async loadConversations() {
      try {
        const resp = await fetch('/api/conversations', {
          headers: { 'Authorization': `Bearer ${TOKEN}` }
        });
        this.conversations = await resp.json();
      } catch(e) {
        console.error('Erro ao carregar conversas:', e);
      }
    },

    async selectConv(conv) {
      this.activeConv = conv;
      this.messages = [];
      try {
        const resp = await fetch(`/api/conversations/${conv.instance_name}/${conv.phone}/messages`, {
          headers: { 'Authorization': `Bearer ${TOKEN}` }
        });
        this.messages = await resp.json();
        this.$nextTick(() => this.scrollToBottom());
      } catch(e) {
        console.error('Erro ao carregar mensagens:', e);
      }
    },

    connectWS() {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      this.ws = new WebSocket(`${proto}//${location.host}/ws?token=${TOKEN}`);

      this.ws.onmessage = (e) => {
        let msg;
        try { msg = JSON.parse(e.data); } catch { return; }
        if (msg.event !== 'new_message') return;

        const d = msg.data;

        // Atualizar lista de conversas
        const idx = this.conversations.findIndex(
          c => c.contact_id === d.contact_id && c.instance_id === d.instance_id
        );
        if (idx > -1) {
          const conv = this.conversations.splice(idx, 1)[0];
          conv.last_message = d.content;
          conv.last_direction = d.direction;
          conv.last_message_at = d.created_at;
          this.conversations.unshift(conv);
        } else {
          // Nova conversa — recarregar lista
          this.loadConversations();
        }

        // Se é a conversa ativa, adicionar mensagem no thread
        if (this.activeConv &&
            this.activeConv.contact_id === d.contact_id &&
            this.activeConv.instance_id === d.instance_id) {
          this.messages.push(d);
          this.$nextTick(() => this.scrollToBottom());
        }
      };

      this.ws.onclose = () => {
        setTimeout(() => this.connectWS(), 3000);
      };
    },

    async sendMessage() {
      if (!this.messageInput.trim() || !this.activeConv || this.sending) return;
      const text = this.messageInput.trim();
      this.messageInput = '';
      this.sending = true;
      try {
        await fetch(`/instances/${this.activeConv.instance_name}/send/text`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${TOKEN}`
          },
          body: JSON.stringify({ number: this.activeConv.phone, message: text })
        });
      } catch(e) {
        this.messageInput = text; // restaurar em caso de erro
        alert('Erro ao enviar: ' + e.message);
      } finally {
        this.sending = false;
      }
    },

    scrollToBottom() {
      const el = document.getElementById('messages-container');
      if (el) el.scrollTop = el.scrollHeight;
    },

    formatTime(isoStr) {
      if (!isoStr) return '';
      const d = new Date(isoStr);
      return d.toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
    },

    formatDate(isoStr) {
      if (!isoStr) return '';
      const d = new Date(isoStr);
      const today = new Date();
      if (d.toDateString() === today.toDateString()) return 'hoje';
      return d.toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit' });
    }
  }
}
</script>

<div x-data="conversationsPage()" x-init="init()" class="flex gap-0 -mx-6 -my-8 h-[calc(100vh-73px)]">

  <!-- Painel esquerdo: lista de conversas -->
  <div class="w-80 flex-shrink-0 bg-white border-r border-gray-200 flex flex-col">
    <div class="px-4 py-3 border-b border-gray-100">
      <h2 class="text-sm font-semibold text-gray-700">Conversas</h2>
    </div>

    <div class="flex-1 overflow-y-auto">
      <template x-if="conversations.length === 0">
        <div class="flex flex-col items-center justify-center h-full text-gray-400 px-4 text-center">
          <p class="text-3xl mb-2">💬</p>
          <p class="text-sm">Nenhuma conversa ainda.</p>
          <p class="text-xs mt-1">Mensagens recebidas aparecerão aqui.</p>
        </div>
      </template>

      <template x-for="conv in conversations" :key="conv.contact_id + conv.instance_id">
        <div @click="selectConv(conv)"
          class="px-4 py-3 cursor-pointer hover:bg-gray-50 border-b border-gray-50 transition-colors"
          :class="activeConv && activeConv.contact_id === conv.contact_id && activeConv.instance_id === conv.instance_id ? 'bg-blue-50 border-l-2 border-l-blue-500' : ''">
          <div class="flex items-start justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <div class="w-8 h-8 rounded-full bg-gray-200 flex items-center justify-center flex-shrink-0 text-xs font-medium text-gray-600"
                x-text="(conv.name || conv.phone).charAt(0).toUpperCase()"></div>
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-800 truncate" x-text="conv.name || conv.phone"></p>
                <p class="text-xs text-gray-400 truncate">
                  <span x-show="conv.last_direction === 'out'" class="text-blue-400">Você: </span>
                  <span x-text="conv.last_message"></span>
                </p>
              </div>
            </div>
            <div class="flex flex-col items-end gap-1 flex-shrink-0">
              <p class="text-xs text-gray-400" x-text="formatDate(conv.last_message_at)"></p>
              <span class="text-xs text-gray-400" x-text="conv.instance_name"></span>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>

  <!-- Painel direito: thread de mensagens -->
  <div class="flex-1 flex flex-col bg-gray-50">

    <!-- Header do chat -->
    <div class="bg-white border-b border-gray-200 px-6 py-3 flex items-center gap-3">
      <template x-if="activeConv">
        <div class="flex items-center gap-3">
          <div class="w-8 h-8 rounded-full bg-gray-200 flex items-center justify-center text-sm font-medium text-gray-600"
            x-text="(activeConv.name || activeConv.phone).charAt(0).toUpperCase()"></div>
          <div>
            <p class="text-sm font-medium text-gray-800" x-text="activeConv.name || activeConv.phone"></p>
            <p class="text-xs text-gray-400">+<span x-text="activeConv.phone"></span> · <span x-text="activeConv.instance_name"></span></p>
          </div>
        </div>
      </template>
      <template x-if="!activeConv">
        <p class="text-sm text-gray-400">Selecione uma conversa</p>
      </template>
    </div>

    <!-- Mensagens -->
    <div id="messages-container" class="flex-1 overflow-y-auto px-6 py-4 space-y-2">
      <template x-if="!activeConv">
        <div class="flex items-center justify-center h-full text-gray-300">
          <p class="text-sm">Selecione uma conversa para ver as mensagens</p>
        </div>
      </template>

      <template x-for="msg in messages" :key="msg.id">
        <div class="flex" :class="msg.direction === 'out' ? 'justify-end' : 'justify-start'">
          <div class="max-w-xs lg:max-w-md px-3 py-2 rounded-2xl text-sm shadow-sm"
            :class="msg.direction === 'out'
              ? 'bg-blue-500 text-white rounded-br-sm'
              : 'bg-white text-gray-800 rounded-bl-sm border border-gray-100'">
            <p x-text="msg.content" class="break-words"></p>
            <p class="text-xs mt-0.5 text-right"
              :class="msg.direction === 'out' ? 'text-blue-200' : 'text-gray-400'"
              x-text="formatTime(msg.created_at)"></p>
          </div>
        </div>
      </template>
    </div>

    <!-- Input de mensagem -->
    <div class="bg-white border-t border-gray-200 px-4 py-3">
      <template x-if="activeConv">
        <div class="flex gap-2">
          <input type="text" x-model="messageInput"
            @keydown.enter.prevent="sendMessage()"
            placeholder="Digite sua mensagem..."
            :disabled="sending"
            class="flex-1 border border-gray-300 rounded-full px-4 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50">
          <button @click="sendMessage()" :disabled="sending || !messageInput.trim()"
            class="bg-blue-500 text-white rounded-full px-4 py-2 text-sm font-medium hover:bg-blue-600 transition-colors disabled:opacity-40">
            <span x-text="sending ? '...' : 'Enviar'"></span>
          </button>
        </div>
      </template>
      <template x-if="!activeConv">
        <div class="text-center text-sm text-gray-300 py-1">Selecione uma conversa para responder</div>
      </template>
    </div>

  </div>
</div>
{{ end }}
```

- [ ] **Step 5: Compilar e reiniciar**

```bash
docker exec -d wapi-dev sh -c "cd /app && go build -o server ./cmd/server && pkill server 2>/dev/null; sleep 1; ./server >> /tmp/wapi.log 2>&1"
sleep 4
```

- [ ] **Step 6: Testar no browser**

Acessar `http://65.21.109.105:8090/conversations`:
1. Painel esquerdo mostra conversas existentes (se houver)
2. Clicar numa conversa → painel direito mostra mensagens
3. Abrir DevTools → Network → WS → verificar conexão WebSocket ativa
4. Enviar uma mensagem de WhatsApp para a instância → deve aparecer em tempo real sem refresh
5. Digitar mensagem no input + Enter → deve enviar e aparecer no thread

- [ ] **Step 7: Commit**

```bash
cd /opt/wapi-refactor
git add web/templates/conversations.html web/templates/layout.html \
        internal/handler/web.go internal/handler/conversations.go cmd/server/main.go
git commit -m "feat: add real-time conversations page with WebSocket and two-pane layout"
```

---

## Self-Review

**Spec coverage:**
- ✅ Página de conversa funcional — Task 6
- ✅ Em tempo real (WebSocket push) — Task 2 (hub) + Task 3 (broadcast ao receber) + Task 6 (cliente WS)
- ✅ Histórico de mensagens persistido — Task 1 (DB) + Task 3 (salvar ao receber) + Task 4 (salvar ao enviar)
- ✅ Lista de conversas ordenada por recência — Task 5 (GET /api/conversations)
- ✅ Thread de mensagens por conversa — Task 5 (GET /api/conversations/:name/:phone/messages)
- ✅ Enviar mensagem pelo UI — Task 6 (input + fetch POST /instances/:name/send/text) + Task 4 (persiste)
- ✅ Grupos ignorados (1:1 apenas) — Task 3 (if !isGroup)

**Placeholder scan:** Nenhum TBD encontrado. Todo código está completo e referencia tipos/funções definidos nas mesmas tasks.

**Type consistency:**
- `contact_id` e `instance_id` usados consistentemente em Tasks 3, 4, 5, e 6
- `direction` sempre `"in"` ou `"out"` (constraint no DB e nos broadcasts)
- `instance_name` usado para lookup em GetMessages e no fetch de envio — consistente com o `Name` do `Instance`
