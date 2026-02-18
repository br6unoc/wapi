# 💬 WAPI — WhatsApp API Manager

API REST multi-instância para integração com WhatsApp via whatsmeow. Suporta múltiplos números simultâneos, cada um com sua própria API Key, webhook e configurações independentes.

## ✨ Funcionalidades

- 🔐 **Multi-instância** — Gerencie múltiplos números WhatsApp simultaneamente
- 📱 **QR Code automático** — Conexão rápida via QR Code
- 📤 **Envio de mensagens** — Texto, imagem, áudio e documentos
- 📥 **Webhook** — Receba mensagens em tempo real via webhook
- 🎙️ **Transcrição de áudio** — Whisper.cpp integrado (~6 segundos)
- 🔑 **API Key individual** — Cada instância tem sua própria chave
- ⚡ **SSE** — Server-Sent Events para atualizações em tempo real
- 🤖 **Humanização** — Simula digitação e status online
- 🐳 **Docker** — Deploy simplificado com Docker Compose

## 🚀 Instalação Rápida

### Pré-requisitos
- Docker e Docker Compose
- Ubuntu/Debian (recomendado)

### Instalação
```bash
git clone https://github.com/br6unoc/wapi.git
cd wapi
chmod +x install.sh
./install.sh
```

O script vai pedir:
- URL do sistema (ex: `https://wapi.seudominio.com`)
- Usuário admin (padrão: `admin`)
- Senha do admin
- Senha do PostgreSQL (gerada automaticamente se vazio)

Após a instalação, acesse a URL configurada e faça login!

## 📚 Documentação da API

Acesse a documentação completa através do painel web em **Documentação** ou veja abaixo os endpoints principais.

### Autenticação

Para envio de mensagens, use o header:
```
apikey: SUA_CHAVE_AQUI
```

Para gerenciamento de instâncias (JWT):
```
Authorization: Bearer SEU_TOKEN
```

### Endpoints Principais

#### Criar Instância
```bash
POST /instances
Authorization: Bearer TOKEN

{
  "name": "minha-instancia"
}
```

#### Enviar Texto
```bash
POST /instances/:name/send/text
apikey: SUA_CHAVE

{
  "number": "5511999999999",
  "message": "Olá! Tudo bem?"
}
```

#### Enviar Mídia
```bash
POST /instances/:name/send/media
apikey: SUA_CHAVE

{
  "number": "5511999999999",
  "media": "base64_aqui",
  "mimetype": "image/jpeg",
  "filename": "foto.jpg",
  "caption": "Legenda opcional"
}
```

#### Webhook

Configure a URL do webhook no painel. Quando uma mensagem é recebida:
```json
{
  "event": "messages.upsert",
  "instance": "minha-instancia",
  "instanceId": "uuid",
  "data": {
    "from": "5511999999999",
    "pushName": "João Silva",
    "message": "Olá!",
    "type": "text",
    "timestamp": "2026-02-17T14:00:00-03:00",
    "messageId": "3A...",
    "transcription": "texto transcrito (apenas áudios)"
  }
}
```

## 🏗️ Arquitetura
```
wapi/
├── cmd/server/          # Servidor principal
├── internal/
│   ├── auth/           # Autenticação JWT
│   ├── handler/        # Handlers HTTP
│   ├── instance/       # Gerenciador de instâncias
│   ├── service/        # Lógica de negócio
│   ├── transcriber/    # Whisper integration
│   └── whatsapp/       # Cliente WhatsApp
├── web/                # Interface web
├── docker/             # Dockerfiles
├── store/              # Persistência
└── config/             # Configurações
```

## 🔧 Configuração

Edite o arquivo `.env`:
```env
PORT=8080
APP_URL=https://wapi.seudominio.com
POSTGRES_PASSWORD=senha_forte_aqui
JWT_SECRET=chave_secreta_aqui
ADMIN_USER=admin
ADMIN_PASSWORD=senha_admin_aqui
```

## 🐳 Docker Compose
```bash
# Iniciar
docker compose up -d

# Ver logs
docker compose logs -f

# Parar
docker compose down

# Rebuild
docker compose up -d --build
```

## 🔒 Segurança

- ⚠️ Sempre use HTTPS em produção
- 🔐 Troque as senhas padrão
- 🛡️ Configure firewall (apenas portas 80, 443, 22)
- 🔑 Mantenha as API Keys seguras
- 📝 Revise logs regularmente

## 🤝 Contribuindo

Contribuições são bem-vindas! Por favor:

1. Fork o projeto
2. Crie uma branch (`git checkout -b feature/nova-funcionalidade`)
3. Commit suas mudanças (`git commit -m 'Adiciona nova funcionalidade'`)
4. Push para a branch (`git push origin feature/nova-funcionalidade`)
5. Abra um Pull Request

## 📝 Licença

MIT License - veja [LICENSE](LICENSE) para mais detalhes.

## 🙏 Créditos

- [whatsmeow](https://github.com/tulir/whatsmeow) - Cliente WhatsApp
- [Whisper.cpp](https://github.com/ggerganov/whisper.cpp) - Transcrição de áudio
- [Gin](https://github.com/gin-gonic/gin) - Framework web

## 📞 Suporte

Para reportar bugs ou solicitar features, abra uma [issue](https://github.com/br6unoc/wapi/issues).

---

Desenvolvido com ❤️ por [br6unoc](https://github.com/br6unoc)
