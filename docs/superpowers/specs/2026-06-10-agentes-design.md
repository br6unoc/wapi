# Spec: Seção Agentes — BotWapp

**Data:** 2026-06-10  
**Status:** Aprovado

## Contexto

O BotWapp é um sistema multi-empresa (Go + Gin + PostgreSQL + whatsmeow + Alpine.js) para gerenciamento de conversas WhatsApp. Cada empresa possui conexões (instâncias whatsmeow), usuários com roles (super_admin, admin, coordinator, consultant), e uma UI SSR com Alpine.js.

Este spec descreve a implementação da seção **Agentes**: IAs conversacionais que respondem automaticamente a mensagens recebidas em conexões WhatsApp, com handoff para atendentes humanos.

---

## Requisitos

### Funcionais

1. Apenas `admin` e `super_admin` podem criar, editar e excluir agentes.
2. Cada empresa pode ter múltiplos agentes.
3. Cada agente é vinculado a uma conexão (instance) e a um tipo de contato:
   - `first_contact` — ativa para contatos que nunca tiveram conversa anterior
   - `returning` — ativa para contatos que já interagiram antes
4. Cada conexão comporta no máximo 1 agente por tipo de contato.
5. O agente responde automaticamente a mensagens de texto recebidas.
6. Handoff para humano ocorre em três situações:
   - **Manual**: atendente clica em "Assumir conversa" na UI
   - **Auto pelo agente**: resposta da AI contém a keyword de handoff configurada (padrão: `PRECISO_DE_HUMANO`)
   - **Auto por atividade humana**: atendente envia qualquer mensagem pela UI
7. Após handoff, o atendente pode devolver a conversa ao agente via "Retornar ao agente".
8. A configuração de provedor de AI (OpenAI ou Groq), modelo e chave API é feita na aba Configurações e é por empresa.

### Não funcionais

- Chamadas à API de AI são feitas em goroutine (não bloqueiam o handler de mensagens).
- O histórico das últimas 20 mensagens do contato é enviado ao modelo para manter contexto conversacional.
- Falhas na chamada de AI são logadas; a conversa continua sem resposta automática (silencioso para o usuário final).

---

## Banco de Dados

### Nova tabela `agents`

Adicionada ao `Migrate()` em `store/postgres/store.go`:

```sql
CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    instance_id UUID NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    prompt TEXT NOT NULL,
    contact_type VARCHAR(20) NOT NULL CHECK (contact_type IN ('first_contact', 'returning')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    handoff_keyword VARCHAR(100) NOT NULL DEFAULT 'PRECISO_DE_HUMANO',
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE (instance_id, contact_type)
);
```

### Alterações na tabela `contacts`

```sql
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS agent_mode BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS is_first_contact BOOLEAN NOT NULL DEFAULT TRUE;
```

- `agent_mode`: controla se o agente responde a este contato. Inicia `TRUE`. Vira `FALSE` em qualquer handoff. Volta a `TRUE` se atendente clicar em "Retornar ao agente".
- `is_first_contact`: `TRUE` enquanto o contato não teve nenhuma resposta do sistema. Vira `FALSE` após o agente (ou humano) enviar a primeira mensagem de saída. Determina qual tipo de agente (`first_contact` ou `returning`) é usado.

---

## Configurações de AI por Empresa

Armazenadas na tabela `settings` existente com chave composta:

| Chave interna | Descrição |
|---|---|
| `{company_id}:ai_provider` | `"openai"` ou `"groq"` |
| `{company_id}:ai_model` | Ex: `"gpt-4o-mini"`, `"llama-3.3-70b-versatile"` |
| `{company_id}:ai_api_key` | Chave de API do provedor |

Novos helpers em `store/postgres/store.go`:

```go
func GetCompanySetting(companyID, key string) (string, error)
func SetCompanySetting(companyID, key, value string) error
```

Implementação: prefixam `key` com `companyID + ":"` e chamam `GetSetting`/`SetSetting` internamente.

---

## Pacote AI — `internal/aiagent/aiagent.go`

```go
package aiagent

type Config struct {
    Provider string // "openai" | "groq"
    Model    string
    APIKey   string
}

type Message struct {
    Role    string // "user" | "assistant"
    Content string
}

// Chat envia historico + system prompt e retorna a resposta do modelo.
func Chat(cfg Config, systemPrompt string, history []Message) (string, error)
```

Implementação interna usa `net/http` com JSON puro (sem SDK externo):
- OpenAI: `POST https://api.openai.com/v1/chat/completions`
- Groq: `POST https://api.groq.com/openai/v1/chat/completions` (formato idêntico ao OpenAI)

O sistema monta o payload como `[{role:"system", content: systemPrompt}, ...history]`.

---

## Handlers — `internal/handler/agent.go`

### CRUD de Agentes (admin only)

| Método | Rota | Handler |
|---|---|---|
| GET | `/api/agents` | `ListAgents` |
| POST | `/api/agents` | `CreateAgent` |
| PATCH | `/api/agents/:id` | `UpdateAgent` |
| DELETE | `/api/agents/:id` | `DeleteAgent` |
| GET | `/agents` | `WebAgents` (página SSR) |

Todos os handlers de API usam `companyID := currentCompanyID(c)` para scoping. `WebAgents` passa a lista de instâncias da empresa para popular o `<select>` de conexão no formulário.

Campos do agente (request/response):

```json
{
  "id": "uuid",
  "name": "Atendente IA - Suporte",
  "prompt": "Você é um assistente...",
  "contact_type": "first_contact",
  "instance_id": "uuid",
  "instance_name": "Suporte WhatsApp",
  "is_active": true,
  "handoff_keyword": "PRECISO_DE_HUMANO"
}
```

Conflito na UNIQUE `(instance_id, contact_type)` retorna HTTP 409 com mensagem: `"já existe um agente deste tipo para esta conexão"`.

### Takeover / Resume

| Método | Rota | Handler | Ação |
|---|---|---|---|
| POST | `/api/conversations/:name/:phone/takeover` | `TakeoverConversation` | `agent_mode = FALSE` |
| POST | `/api/conversations/:name/:phone/resume` | `ResumeAgent` | `agent_mode = TRUE` |

Ambos retornam `{"ok": true}` e fazem broadcast WS com evento `agent_mode_changed` para atualizar a UI em tempo real.

---

## Lógica de Resposta — `internal/instance/manager.go`

### Ponto de entrada

Para mensagens de texto (não-grupo), `processMessage` atualmente chama `go inst.saveMessage(...)` em goroutine. Para que o agente tenha o `contactID`, a chamada para texto precisa mudar: em vez de `go inst.saveMessage(...)` sozinho, chamar uma nova função `go inst.saveMessageAndReply(senderNumber, pushName, content, msgID)` que (a) salva a mensagem e obtém o contactID via upsert, e (b) em seguida chama `tryAgentReply(contactID, ...)` — tudo no mesmo goroutine, sequencialmente.

```go
// em processMessage, substituindo o `go inst.saveMessage(...)` para texto:
if !isGroup {
    go inst.saveMessageAndReply(senderNumber, v.Info.PushName, content, v.Info.ID)
}
```

Para mensagens de mídia (áudio, imagem, vídeo, documento), o agente **não** responde — apenas mensagens de texto ativam o agente. (Comportamento pode ser expandido no futuro.)

### `tryAgentReply` — fluxo completo

```
1. SELECT agent_mode, is_first_contact FROM contacts WHERE id = $1
   → se agent_mode = FALSE: retorna (sem ação)

2. contactType = "first_contact" se is_first_contact, senão "returning"

3. SELECT * FROM agents WHERE instance_id = $1 AND contact_type = $2 AND is_active = TRUE
   → se não encontrar: retorna (conexão sem agente configurado)

4. Carrega config AI: GetCompanySetting(companyID, "ai_provider/model/api_key")
   → se faltando algum campo: loga aviso e retorna

5. Carrega histórico: SELECT direction, content FROM messages
   WHERE contact_id = $1 AND type = 'text'
   ORDER BY created_at DESC LIMIT 20
   → converte: direction 'in' → role "user", 'out' → role "assistant"
   → inverte ordem (mais antigas primeiro)

6. aiResp, err := aiagent.Chat(cfg, agent.Prompt, history)
   → se err != nil: loga e retorna (silencioso)

7. Se strings.Contains(aiResp, agent.HandoffKeyword):
   UPDATE contacts SET agent_mode = FALSE WHERE id = $1
   aiResp = strings.ReplaceAll(aiResp, agent.HandoffKeyword, "")
   aiResp = strings.TrimSpace(aiResp)
   → broadcast WS evento agent_mode_changed

8. UPDATE contacts SET is_first_contact = FALSE WHERE id = $1

9. Salva aiResp como mensagem 'out' no DB
10. Broadcast WS com evento new_message
11. service.SendText(inst, phone, aiResp)
```

### Desativação por atividade humana

Em `SendFromUI` e `SendMediaFromUI` (após salvar a mensagem de saída):

```go
postgres.DB.Exec(`UPDATE contacts SET agent_mode = FALSE WHERE id = $1`, contactID)
hub.Global.BroadcastToCompany(inst.CompanyID, agentModeChangedPayload(contactID, false))
```

---

## Frontend

### `web/templates/agents.html` (novo)

Segue o padrão de `sectors.html`:
- Tabela listando agentes: Nome, Conexão, Tipo, Status (ativo/inativo), ações (editar, excluir)
- Formulário inline ou modal Alpine.js para criar/editar
- Campos: nome, conexão (select populado pelas instâncias da empresa), tipo de contato (radio: "Primeiro contato" / "Demais contatos"), prompt (textarea), keyword de handoff (input, com valor padrão), toggle ativo/inativo
- Ao salvar, valida que os campos obrigatórios estão preenchidos antes de submeter
- Erro 409 é exibido inline: "Já existe um agente deste tipo para esta conexão"

### `web/templates/layout.html`

Novo item no bloco `{{ if or (eq .Role "admin") (eq .Role "super_admin") }}`:

```html
<a href="/agents" ...>
  <!-- ícone robô SVG -->
  Agentes
</a>
```

### `web/templates/settings.html`

Nova seção "Agente de IA", renderizada somente para admin/super_admin, dentro do mesmo form ou como seção separada:

- Select: Provedor (OpenAI / Groq)
- Input texto: Modelo (placeholder dinâmico por provedor via Alpine.js)
- Input password: Chave API
- Botão Salvar próprio (POST para `/settings`)

### `web/templates/conversations.html`

- `ListConversations` passa a incluir `agent_mode bool` no JSON.
- Na UI da conversa aberta, quando `agent_mode = true` e a conexão tem agente configurado: exibir badge "Agente ativo" + botão "Assumir conversa".
- Quando `agent_mode = false`: exibir botão "Retornar ao agente" (se houver agente configurado na conexão).
- Evento WS `agent_mode_changed` atualiza o estado dos botões em tempo real sem reload.

---

## Rotas — `cmd/server/main.go`

```go
// Agentes (admin only)
agentsAPI := r.Group("/api/agents", handler.AuthMiddleware(), handler.AdminOrAbove())
agentsAPI.GET("", handler.ListAgents)
agentsAPI.POST("", handler.CreateAgent)
agentsAPI.PATCH("/:id", handler.UpdateAgent)
agentsAPI.DELETE("/:id", handler.DeleteAgent)

// Takeover / resume (qualquer usuário autenticado)
apiGroup.POST("/conversations/:name/:phone/takeover", handler.TakeoverConversation)
apiGroup.POST("/conversations/:name/:phone/resume", handler.ResumeAgent)

// Web
webGroup.GET("/agents", handler.WebAgents)
```

---

## Resumo de arquivos impactados

| Arquivo | Tipo | Mudança |
|---|---|---|
| `store/postgres/store.go` | Modificar | Migration `agents` + colunas `contacts` + helpers `GetCompanySetting/SetCompanySetting` |
| `internal/aiagent/aiagent.go` | **Novo** | Wrapper OpenAI/Groq via HTTP |
| `internal/handler/agent.go` | **Novo** | CRUD agentes + WebAgents + TakeoverConversation + ResumeAgent |
| `internal/instance/manager.go` | Modificar | `processMessage` → `tryAgentReply`; `SendFromUI/SendMediaFromUI` → desativa agent_mode |
| `internal/handler/conversations.go` | Modificar | `ListConversations` inclui `agent_mode`; `SendFromUI/SendMediaFromUI` desativam agent_mode |
| `internal/handler/settings.go` | Modificar | Salva/lê config AI por empresa |
| `web/templates/agents.html` | **Novo** | CRUD de agentes |
| `web/templates/layout.html` | Modificar | Link "Agentes" no nav |
| `web/templates/settings.html` | Modificar | Seção "Agente de IA" |
| `web/templates/conversations.html` | Modificar | Botões takeover/resume + evento WS |
| `cmd/server/main.go` | Modificar | Rotas agents API + web |

---

## Fora do escopo (v1)

- Agente respondendo a mensagens de mídia (áudio, imagem, vídeo)
- Histórico de conversas entre sessões de agente (quem respondeu o quê)
- Reativação automática do agente após timeout sem resposta humana
- Múltiplas keywords de handoff
- Limite de rate / custo de API
