# Frontend Conexões: Tela de Gerenciamento de Conexões

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a web UI for managing WhatsApp connections (instances) with real-time QR Code scanning and status updates.

**Architecture:** Go html/template renders the initial page (SSR) with the instance list. Alpine.js handles interactivity: connect/disconnect actions, SSE for real-time status/QR Code, and a modal for QR scanning. JWT stored in a browser cookie (`wapi_token`) for web session auth — the web middleware reads the cookie and validates it via `auth.ValidateToken`. WAPI's existing REST API handles all backend operations. No npm — CDN only.

**Tech Stack:** Go + Gin + html/template, Alpine.js v3 (CDN), Tailwind CSS v3 (CDN), qrcode.js v1.5 (CDN)

---

## File Map

| File | Ação | Responsabilidade |
|------|------|-----------------|
| `web/templates/layout.html` | Criar | Shell HTML base, CDN scripts, navbar |
| `web/templates/login.html` | Criar | Formulário de login |
| `web/templates/connections.html` | Criar | Lista de conexões + modal QR + modal criar |
| `internal/handler/web.go` | Criar | Handlers de template (login, connections, logout) + WebAuthMiddleware |
| `cmd/server/main.go` | Modificar | Adicionar rotas web, LoadTemplates, redirecionar `/` |

---

## Task 1: Template engine + layout base

**Files:**
- Create: `web/templates/layout.html`
- Create: `internal/handler/web.go` (initial skeleton)
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Criar diretório de templates e layout.html**

Criar `web/templates/layout.html`:

```html
<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta name="auth-token" content="{{ .Token }}">
  <title>{{ block "title" . }}WAPI{{ end }}</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/qrcode@1.5.3/build/qrcode.min.js"></script>
</head>
<body class="bg-gray-50 min-h-screen">
  <nav class="bg-white border-b border-gray-200 px-6 py-4">
    <div class="flex items-center justify-between max-w-6xl mx-auto">
      <span class="text-lg font-semibold text-gray-800">WAPI</span>
      {{ if .Token }}
      <a href="/logout" class="text-sm text-gray-500 hover:text-gray-800 transition-colors">Sair</a>
      {{ end }}
    </div>
  </nav>
  <main class="max-w-6xl mx-auto px-6 py-8">
    {{ block "content" . }}{{ end }}
  </main>
</body>
</html>
```

- [ ] **Step 2: Criar web.go com skeleton inicial**

Criar `internal/handler/web.go`:

```go
package handler

import (
	"html/template"
	"net/http"
	"wapi/internal/auth"

	"github.com/gin-gonic/gin"
)

var templates *template.Template

func LoadTemplates() {
	templates = template.Must(template.ParseGlob("web/templates/*.html"))
}

func render(c *gin.Context, status int, name string, data gin.H) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(c.Writer, name, data); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
	}
}

func WebAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie("wapi_token")
		if err != nil || token == "" {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		if _, err := auth.ValidateToken(token); err != nil {
			c.SetCookie("wapi_token", "", -1, "/", "", false, false)
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Set("token", token)
		c.Next()
	}
}

func WebLogin(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		render(c, http.StatusOK, "login.html", gin.H{})
		return
	}
	// POST: Task 2
}

func WebLogout(c *gin.Context) {
	c.SetCookie("wapi_token", "", -1, "/", "", false, false)
	c.Redirect(http.StatusFound, "/login")
}

func WebConnections(c *gin.Context) {
	// Task 3
	token, _ := c.Get("token")
	render(c, http.StatusOK, "connections.html", gin.H{"Token": token})
}
```

- [ ] **Step 3: Modificar main.go — adicionar rotas web**

Em `cmd/server/main.go`, **após** o bloco `instances` e **antes** de `r.StaticFile(...)`, adicionar:

```go
// Web UI
handler.LoadTemplates()
r.GET("/login", handler.WebLogin)
r.POST("/login", handler.WebLogin)
r.GET("/logout", handler.WebLogout)
r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/connections") })

webGroup := r.Group("/", handler.WebAuthMiddleware())
{
    webGroup.GET("/connections", handler.WebConnections)
}
```

Remover (ou comentar) a linha antiga:
```go
// r.StaticFile("/", "./web/index.html")
```

- [ ] **Step 4: Compilar para verificar**

```bash
docker exec wapi-dev sh -c "cd /opt/wapi-refactor && go build ./..."
```

Esperado: sem output (sucesso).

- [ ] **Step 5: Commit**

```bash
cd /opt/wapi-refactor
git add web/templates/layout.html internal/handler/web.go cmd/server/main.go
git commit -m "feat: setup template engine and base layout for web UI"
```

---

## Task 2: Login page + autenticação por cookie

**Files:**
- Create: `web/templates/login.html`
- Modify: `internal/handler/web.go` (completar WebLogin POST)

- [ ] **Step 1: Criar login.html**

Criar `web/templates/login.html`:

```html
{{ template "layout.html" . }}
{{ define "title" }}Login — WAPI{{ end }}
{{ define "content" }}
<div class="flex items-center justify-center min-h-64 mt-16">
  <div class="bg-white rounded-xl border border-gray-200 shadow-sm p-8 w-full max-w-sm">
    <h1 class="text-xl font-semibold text-gray-800 mb-6">Entrar</h1>
    {{ if .Error }}
    <div class="mb-4 text-sm text-red-600 bg-red-50 border border-red-200 rounded-lg px-4 py-3">
      {{ .Error }}
    </div>
    {{ end }}
    <form method="POST" action="/login" class="space-y-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Usuário</label>
        <input type="text" name="username" autocomplete="username" required
          class="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500">
      </div>
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Senha</label>
        <input type="password" name="password" autocomplete="current-password" required
          class="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500">
      </div>
      <button type="submit"
        class="w-full bg-blue-600 text-white rounded-lg px-4 py-2 text-sm font-medium hover:bg-blue-700 transition-colors">
        Entrar
      </button>
    </form>
  </div>
</div>
{{ end }}
```

- [ ] **Step 2: Completar WebLogin POST em web.go**

Adicionar imports `wapi/internal/auth` já existe. Substituir a função `WebLogin` em `internal/handler/web.go`:

```go
func WebLogin(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		render(c, http.StatusOK, "login.html", gin.H{})
		return
	}

	username := c.PostForm("username")
	password := c.PostForm("password")

	token, err := auth.Login(username, password)
	if err != nil {
		render(c, http.StatusUnauthorized, "login.html", gin.H{"Error": "Usuário ou senha inválidos"})
		return
	}

	// cookie de 24h, não HttpOnly para que Alpine.js possa ler se necessário
	c.SetCookie("wapi_token", token, 86400, "/", "", false, false)
	c.Redirect(http.StatusFound, "/connections")
}
```

- [ ] **Step 3: Compilar e reiniciar**

```bash
docker exec wapi-dev sh -c "cd /opt/wapi-refactor && go build -o server ./cmd/server && kill -SIGTERM \$(pgrep server); sleep 1 && ./server &"
```

- [ ] **Step 4: Testar login no browser**

Acessar `http://65.21.109.105:8090/login`.
- Login com `admin` / `admin123` → deve redirecionar para `/connections`
- Login com senha errada → deve mostrar mensagem de erro em vermelho

- [ ] **Step 5: Commit**

```bash
cd /opt/wapi-refactor
git add web/templates/login.html internal/handler/web.go
git commit -m "feat: add login page with cookie-based JWT session"
```

---

## Task 3: Página de conexões (SSR)

**Files:**
- Create: `web/templates/connections.html`
- Modify: `internal/handler/web.go` (completar WebConnections)

- [ ] **Step 1: Completar WebConnections handler**

Substituir a função `WebConnections` em `internal/handler/web.go` e adicionar import `wapi/internal/instance`:

```go
func WebConnections(c *gin.Context) {
	token, _ := c.Get("token")

	type InstView struct {
		Name   string
		Status string
		Phone  string
	}

	all := instance.Global.GetAll()
	views := make([]InstView, 0, len(all))
	for _, inst := range all {
		status := "disconnected"
		if inst.Status == "connected" && inst.Phone != "" {
			status = "connected"
		}
		views = append(views, InstView{
			Name:   inst.Name,
			Status: status,
			Phone:  inst.Phone,
		})
	}

	render(c, http.StatusOK, "connections.html", gin.H{
		"Token":     token,
		"Instances": views,
	})
}
```

- [ ] **Step 2: Criar connections.html (estrutura estática)**

Criar `web/templates/connections.html`:

```html
{{ template "layout.html" . }}
{{ define "title" }}Conexões — WAPI{{ end }}
{{ define "content" }}
<div class="flex items-center justify-between mb-6">
  <h1 class="text-xl font-semibold text-gray-800">Conexões WhatsApp</h1>
  <button onclick="document.getElementById('modal-criar').classList.remove('hidden')"
    class="bg-blue-600 text-white px-4 py-2 rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors">
    + Nova conexão
  </button>
</div>

{{ if not .Instances }}
<div class="text-center py-16 text-gray-400">
  <p class="text-4xl mb-3">📱</p>
  <p class="text-sm">Nenhuma conexão criada ainda.</p>
  <p class="text-sm">Clique em <strong>+ Nova conexão</strong> para começar.</p>
</div>
{{ end }}

<div class="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
  {{ range .Instances }}
  <!-- Card da instância — interatividade adicionada na Task 4 -->
  <div class="bg-white rounded-xl border border-gray-200 shadow-sm p-5">
    <div class="flex items-center justify-between mb-3">
      <span class="font-medium text-gray-800">{{ .Name }}</span>
      <span class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full
        {{ if eq .Status "connected" }}bg-green-50 text-green-700{{ else }}bg-gray-100 text-gray-500{{ end }}">
        <span class="w-1.5 h-1.5 rounded-full
          {{ if eq .Status "connected" }}bg-green-500{{ else }}bg-gray-400{{ end }}"></span>
        {{ if eq .Status "connected" }}Conectado{{ else }}Desconectado{{ end }}
      </span>
    </div>
    {{ if .Phone }}
    <p class="text-sm text-gray-500 mb-4">+{{ .Phone }}</p>
    {{ else }}
    <p class="text-sm text-gray-400 mb-4">—</p>
    {{ end }}
    <div class="flex gap-2">
      {{ if eq .Status "connected" }}
      <button class="flex-1 text-sm border border-gray-300 rounded-lg px-3 py-1.5 hover:bg-gray-50 transition-colors text-gray-600">
        Desconectar
      </button>
      {{ else }}
      <button class="flex-1 text-sm bg-green-600 text-white rounded-lg px-3 py-1.5 hover:bg-green-700 transition-colors">
        Conectar
      </button>
      {{ end }}
      <button class="text-sm border border-red-200 text-red-500 rounded-lg px-3 py-1.5 hover:bg-red-50 transition-colors">
        Remover
      </button>
    </div>
  </div>
  {{ end }}
</div>

<!-- Modal: Criar conexão (Task 4) -->
<div id="modal-criar" class="hidden fixed inset-0 bg-black/40 flex items-center justify-center z-50">
  <div class="bg-white rounded-xl border border-gray-200 shadow-xl p-6 w-full max-w-sm mx-4">
    <h2 class="text-base font-semibold text-gray-800 mb-4">Nova conexão</h2>
    <form id="form-criar" class="space-y-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Nome da conexão</label>
        <input type="text" id="input-nome" placeholder="ex: vendas, suporte..." required
          class="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
      </div>
      <div class="flex gap-2">
        <button type="button" onclick="document.getElementById('modal-criar').classList.add('hidden')"
          class="flex-1 border border-gray-300 text-gray-600 rounded-lg px-4 py-2 text-sm hover:bg-gray-50">
          Cancelar
        </button>
        <button type="submit"
          class="flex-1 bg-blue-600 text-white rounded-lg px-4 py-2 text-sm font-medium hover:bg-blue-700">
          Criar
        </button>
      </div>
    </form>
  </div>
</div>
{{ end }}
```

- [ ] **Step 3: Compilar e reiniciar**

```bash
docker exec wapi-dev sh -c "cd /opt/wapi-refactor && go build -o server ./cmd/server && kill -SIGTERM \$(pgrep server); sleep 1 && ./server &"
```

- [ ] **Step 4: Testar no browser**

Acessar `http://65.21.109.105:8090/connections`.
- Deve listar as instâncias existentes com status e telefone corretos (SSR)
- Instâncias conectadas: badge verde + número
- Instâncias desconectadas: badge cinza

- [ ] **Step 5: Commit**

```bash
cd /opt/wapi-refactor
git add web/templates/connections.html internal/handler/web.go
git commit -m "feat: add connections page with SSR instance list"
```

---

## Task 4: Interatividade Alpine.js — criar, conectar, desconectar, QR Code

**Files:**
- Modify: `web/templates/connections.html` (adicionar Alpine.js components, QR modal, ações)

Esta task substitui os botões estáticos por componentes Alpine.js reativos que chamam a API e recebem atualizações em tempo real via SSE.

- [ ] **Step 1: Substituir connections.html pela versão interativa completa**

Substituir `web/templates/connections.html` por:

```html
{{ template "layout.html" . }}
{{ define "title" }}Conexões — WAPI{{ end }}
{{ define "content" }}

<script>
const TOKEN = document.querySelector('meta[name="auth-token"]')?.content || '';

function instanceCard(name, initialStatus, initialPhone) {
  return {
    name,
    status: initialStatus,
    phone: initialPhone,
    loading: false,
    showQR: false,
    qrData: null,
    es: null,

    async connect() {
      this.loading = true;
      try {
        await fetch(`/instances/${this.name}/connect`, {
          method: 'POST',
          headers: { 'Authorization': `Bearer ${TOKEN}` }
        });
        this.startSSE();
      } catch(e) {
        alert('Erro ao conectar: ' + e.message);
      } finally {
        this.loading = false;
      }
    },

    async disconnect() {
      if (!confirm(`Desconectar ${this.name}?`)) return;
      this.loading = true;
      try {
        await fetch(`/instances/${this.name}/disconnect`, {
          method: 'POST',
          headers: { 'Authorization': `Bearer ${TOKEN}` }
        });
        this.status = 'disconnected';
        this.phone = '';
        this.showQR = false;
        if (this.es) { this.es.close(); this.es = null; }
      } finally {
        this.loading = false;
      }
    },

    async remove() {
      if (!confirm(`Remover a conexão "${this.name}"? Esta ação não pode ser desfeita.`)) return;
      await fetch(`/instances/${this.name}`, {
        method: 'DELETE',
        headers: { 'Authorization': `Bearer ${TOKEN}` }
      });
      window.location.reload();
    },

    startSSE() {
      if (this.es) this.es.close();
      this.es = new EventSource(`/instances/${this.name}/sse`);
      this.es.onmessage = (e) => {
        let msg;
        try { msg = JSON.parse(e.data); } catch { return; }

        if (msg.event === 'qr') {
          this.qrData = msg.data.qrcode;
          this.showQR = true;
          this.$nextTick(() => {
            const canvas = document.getElementById(`qr-canvas-${this.name}`);
            if (canvas && this.qrData) {
              QRCode.toCanvas(canvas, this.qrData, { width: 240, margin: 1 }, () => {});
            }
          });
        } else if (msg.event === 'connected') {
          this.status = 'connected';
          this.phone = msg.data.phone || '';
          this.showQR = false;
          this.qrData = null;
          if (this.es) { this.es.close(); this.es = null; }
        } else if (msg.event === 'disconnected') {
          this.status = 'disconnected';
          this.phone = '';
        }
      };
    },

    closeQR() {
      this.showQR = false;
      if (this.es) { this.es.close(); this.es = null; }
    }
  }
}

function createInstance() {
  return {
    showModal: false,
    nome: '',
    loading: false,

    async submit() {
      if (!this.nome.trim()) return;
      this.loading = true;
      try {
        const resp = await fetch('/instances', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': `Bearer ${TOKEN}`
          },
          body: JSON.stringify({ name: this.nome.trim() })
        });
        if (!resp.ok) {
          const err = await resp.json();
          alert('Erro: ' + (err.error || 'falha ao criar'));
          return;
        }
        window.location.reload();
      } finally {
        this.loading = false;
      }
    }
  }
}
</script>

<div x-data="createInstance()">

  <div class="flex items-center justify-between mb-6">
    <h1 class="text-xl font-semibold text-gray-800">Conexões WhatsApp</h1>
    <button @click="showModal = true"
      class="bg-blue-600 text-white px-4 py-2 rounded-lg text-sm font-medium hover:bg-blue-700 transition-colors">
      + Nova conexão
    </button>
  </div>

  {{ if not .Instances }}
  <div class="text-center py-16 text-gray-400">
    <p class="text-4xl mb-3">📱</p>
    <p class="text-sm">Nenhuma conexão criada ainda.</p>
    <p class="text-sm mt-1">Clique em <strong>+ Nova conexão</strong> para começar.</p>
  </div>
  {{ end }}

  <div class="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-3">
    {{ range .Instances }}
    <div x-data="instanceCard('{{ .Name }}', '{{ .Status }}', '{{ .Phone }}')"
         class="bg-white rounded-xl border border-gray-200 shadow-sm p-5">

      <!-- Header: nome + status -->
      <div class="flex items-center justify-between mb-3">
        <span class="font-medium text-gray-800">{{ .Name }}</span>
        <span class="inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full"
          :class="status === 'connected'
            ? 'bg-green-50 text-green-700'
            : 'bg-gray-100 text-gray-500'">
          <span class="w-1.5 h-1.5 rounded-full"
            :class="status === 'connected' ? 'bg-green-500' : 'bg-gray-400'"></span>
          <span x-text="status === 'connected' ? 'Conectado' : 'Desconectado'"></span>
        </span>
      </div>

      <!-- Telefone -->
      <p class="text-sm text-gray-500 mb-4 min-h-[1.25rem]">
        <span x-text="phone ? '+' + phone : '—'"></span>
      </p>

      <!-- Botões -->
      <div class="flex gap-2">
        <button x-show="status !== 'connected'" @click="connect()" :disabled="loading"
          class="flex-1 text-sm bg-green-600 text-white rounded-lg px-3 py-1.5 hover:bg-green-700 transition-colors disabled:opacity-50">
          <span x-text="loading ? 'Aguarde...' : 'Conectar'"></span>
        </button>
        <button x-show="status === 'connected'" @click="disconnect()" :disabled="loading"
          class="flex-1 text-sm border border-gray-300 rounded-lg px-3 py-1.5 hover:bg-gray-50 transition-colors text-gray-600 disabled:opacity-50">
          <span x-text="loading ? 'Aguarde...' : 'Desconectar'"></span>
        </button>
        <button @click="remove()"
          class="text-sm border border-red-200 text-red-500 rounded-lg px-3 py-1.5 hover:bg-red-50 transition-colors">
          Remover
        </button>
      </div>

      <!-- Modal QR Code -->
      <div x-show="showQR" x-cloak
        class="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
        <div class="bg-white rounded-xl shadow-xl p-6 max-w-xs w-full mx-4 text-center">
          <h3 class="font-semibold text-gray-800 mb-1">Escanear QR Code</h3>
          <p class="text-sm text-gray-500 mb-4">Abra o WhatsApp → Dispositivos → Adicionar dispositivo</p>
          <canvas :id="'qr-canvas-' + name" class="mx-auto rounded-lg"></canvas>
          <p class="text-xs text-gray-400 mt-3 mb-4">Aguardando escaneamento...</p>
          <button @click="closeQR()"
            class="text-sm text-gray-500 hover:text-gray-700 underline">
            Cancelar
          </button>
        </div>
      </div>

    </div>
    {{ end }}
  </div>

  <!-- Modal: Criar conexão -->
  <div x-show="showModal" x-cloak
    class="fixed inset-0 bg-black/40 flex items-center justify-center z-50">
    <div class="bg-white rounded-xl border border-gray-200 shadow-xl p-6 w-full max-w-sm mx-4">
      <h2 class="text-base font-semibold text-gray-800 mb-4">Nova conexão</h2>
      <form @submit.prevent="submit()" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Nome da conexão</label>
          <input type="text" x-model="nome" placeholder="ex: vendas, suporte..." required
            class="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500">
        </div>
        <div class="flex gap-2">
          <button type="button" @click="showModal = false; nome = ''"
            class="flex-1 border border-gray-300 text-gray-600 rounded-lg px-4 py-2 text-sm hover:bg-gray-50">
            Cancelar
          </button>
          <button type="submit" :disabled="loading"
            class="flex-1 bg-blue-600 text-white rounded-lg px-4 py-2 text-sm font-medium hover:bg-blue-700 disabled:opacity-50">
            <span x-text="loading ? 'Criando...' : 'Criar'"></span>
          </button>
        </div>
      </form>
    </div>
  </div>

</div>
{{ end }}
```

- [ ] **Step 2: Adicionar `[x-cloak]` style no layout.html**

No `<head>` do `web/templates/layout.html`, adicionar antes de `</head>`:

```html
<style>[x-cloak]{display:none!important}</style>
```

- [ ] **Step 3: Compilar e reiniciar**

```bash
docker exec wapi-dev sh -c "cd /opt/wapi-refactor && go build -o server ./cmd/server && kill -SIGTERM \$(pgrep server); sleep 1 && ./server &"
```

- [ ] **Step 4: Testar fluxo completo**

Acessar `http://65.21.109.105:8090/connections`:

1. Clicar em **+ Nova conexão** → preencher nome → Criar → página recarrega com novo card
2. Clicar **Conectar** num card → modal QR Code aparece → escanear com WhatsApp → status vira "Conectado" e mostra número
3. Clicar **Desconectar** → confirmar → status vira "Desconectado"
4. Clicar **Remover** → confirmar → instância desaparece da lista

- [ ] **Step 5: Commit**

```bash
cd /opt/wapi-refactor
git add web/templates/connections.html web/templates/layout.html
git commit -m "feat: add interactive connections page with Alpine.js, SSE, and QR Code modal"
```

---

## Self-Review

**Spec coverage:**
- ✅ Listar instâncias com status real-time — Task 3 (SSR) + Task 4 (SSE)
- ✅ Criar nova instância — Task 4 (modal criar)
- ✅ Conectar via QR Code — Task 4 (SSE + modal QR)
- ✅ Desconectar — Task 4 (botão desconectar)
- ✅ Remover instância — Task 4 (botão remover)
- ✅ Autenticação web (cookie JWT) — Task 2
- ✅ Redirecionar `/` → `/connections` — Task 1

**Placeholder scan:** Nenhum TBD ou TODO encontrado. Todo o código está completo.

**Type consistency:** `InstView.Name/Status/Phone` usados consistentemente no handler e no template. Funções Alpine `instanceCard` e `createInstance` usam os mesmos campos em todos os steps.
