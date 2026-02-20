# 💬 WAPI — WhatsApp API Manager

API REST multi-instância para integração com WhatsApp via whatsmeow. Suporte a múltiplos números simultâneos, cada um com sua própria API Key, webhook e configurações independentes.

> **⚠️ PRÉ-REQUISITOS OBRIGATÓRIOS**  
> Este instalador foi desenvolvido para funcionar com o **[SetupOrion](https://github.com/oriondesign2015/SetupOrion)**.  
> Você **PRECISA** ter os seguintes serviços rodando no Docker Swarm **ANTES** de instalar o WAPI:
> 
> - ✅ **Traefik** — Proxy reverso com SSL automático
> - ✅ **Portainer** — Gerenciamento de containers
> - ✅ **PostgreSQL** — Banco de dados global compartilhado
> - ✅ **Redis** — Cache global compartilhado
> 
> **Se você ainda NÃO tem esses serviços**, instale primeiro usando o SetupOrion:
> ```bash
> bash <(curl -sSL setup.oriondesign.art.br)
> ```
> Depois instale pelo menos: Traefik, Portainer, PostgreSQL e Redis.

## ✨ Funcionalidades

- 🔐 **Multi-instância** — Gerencie múltiplos números WhatsApp simultaneamente
- 📱 **QR Code automático** — Conexão rápida via QR Code  
- 📤 **Envio de mensagens** — Texto, imagem, áudio e documentos
- 📥 **Webhook** — Receba mensagens em tempo real
- 🎙️ **Transcrição de áudio** — Whisper.cpp integrado (~6 segundos)
- 🔑 **API Key individual** — Cada instância tem sua própria chave
- ⚡ **SSE** — Server-Sent Events para atualizações em tempo real
- 🐳 **Docker Swarm** — Compatível com SetupOrion

## 🚀 Instalação

### 1. Clone o repositório
```bash
git clone https://github.com/br6unoc/wapi.git
cd wapi
```

### 2. Execute o instalador
```bash
chmod +x install.sh
./install.sh
```

### 3. Responda as perguntas

O instalador vai perguntar:
- **Domínio** (ex: `wapi.seudominio.com`)
- **Usuário admin** (padrão: `admin`)
- **Senha do admin** (será gerada automaticamente se vazio)
- **Nome da rede do Traefik** (padrão: `traefik-public`)
- **Nome do certresolver** (padrão: `letsencrypt`)

### 4. Aguarde ~30 segundos

O sistema será instalado automaticamente!

Acesse: `https://seudominio.com` e faça login com as credenciais exibidas.

## 📚 Documentação da API

### Autenticação

**Para envio de mensagens**, use o header:
```
apikey: SUA_CHAVE_AQUI
```

**Para gerenciamento** (criar/listar instâncias), use JWT:
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

Configure a URL do webhook no painel. Formato do evento:
```json
{
  "event": "messages.upsert",
  "instance": "minha-instancia",
  "data": {
    "from": "5511999999999",
    "pushName": "João Silva",
    "message": "Olá!",
    "type": "text",
    "timestamp": "2026-02-19T10:00:00-03:00",
    "messageId": "3A...",
    "transcription": "texto transcrito (apenas áudios)"
  }
}
```

## 🏗️ Arquitetura

O WAPI usa a infraestrutura compartilhada do SetupOrion:
```
WAPI Stack
├── wapi_app (Go + Gin)
│   ├── Conecta: postgres_postgres (global)
│   ├── Conecta: redis_redis (global)
│   └── Exposto: Traefik (SSL automático)
│
└── wapi_whisper (Whisper.cpp)
    └── Transcrição de áudios
```

## 🔧 Gerenciamento

### Ver status dos serviços
```bash
docker service ls | grep wapi
```

### Ver logs
```bash
docker service logs wapi_app -f
```

### Atualizar
```bash
docker service update --image br6unoc/wapi:latest wapi_app
```

### Remover
```bash
docker stack rm wapi
```

## 🔒 Segurança

- ⚠️ Sempre use HTTPS em produção (Traefik configura automaticamente)
- 🔐 Troque as senhas padrão
- 🔑 Mantenha as API Keys seguras
- 📝 Revise logs regularmente

## 🤝 Contribuindo

Contribuições são bem-vindas!

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
- [SetupOrion](https://github.com/oriondesign2015/SetupOrion) - Infraestrutura base

## 📞 Suporte

Para reportar bugs ou solicitar features, abra uma [issue](https://github.com/br6unoc/wapi/issues).

---

**Compatível com SetupOrion v2.8+**  
Desenvolvido com ❤️ por [br6unoc](https://github.com/br6unoc)
