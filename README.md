# 💬 WAPI — WhatsApp API Manager

API REST multi-instância para integração com WhatsApp via whatsmeow. Suporte a múltiplos números simultâneos, cada um com sua própria API Key, webhook e configurações independentes.

> **⚠️ PRÉ-REQUISITOS**  
> Você **PRECISA** ter os seguintes serviços rodando no Docker Swarm **ANTES** de instalar o WAPI:
> 
> - ✅ **Docker Swarm** ativo
> - ✅ **Traefik** (proxy reverso + SSL)
> - ✅ **Portainer** (gerenciamento de containers)
> - ✅ **PostgreSQL** (banco de dados)
> - ✅ **Redis** (cache)
> 
> **Recomendação:** Use o [SetupOrion](https://github.com/oriondesign2015/SetupOrion) para instalar esses serviços facilmente:
> ```bash
> bash <(curl -sSL setup.oriondesign.art.br)
> ```

## ✨ Funcionalidades

- 🔐 **Multi-instância** — Gerencie múltiplos números WhatsApp simultaneamente
- 📱 **QR Code automático** — Conexão rápida via QR Code  
- 📤 **Envio de mensagens** — Texto, imagem, áudio e documentos
- 📥 **Webhook** — Receba mensagens em tempo real
- 🎙️ **Transcrição de áudio** — Whisper.cpp integrado (~6 segundos)
- 🔑 **API Key individual** — Cada instância tem sua própria chave
- ⚡ **SSE** — Server-Sent Events para atualizações em tempo real
- 🐳 **Docker Swarm** — Deploy simplificado

## 🚀 Instalação

Execute os comandos abaixo na sua VPS:
```bash
git clone https://github.com/br6unoc/wapi.git
cd wapi
./install.sh
```

O instalador vai perguntar:
- **Domínio** (ex: `wapi.seudominio.com`)
- **Usuário admin** (padrão: `admin`)
- **Senha do admin** (será gerada automaticamente se deixar vazio)

Aguarde ~40 segundos e acesse: `https://seudominio.com`

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

O WAPI usa a infraestrutura compartilhada do Docker Swarm:
```
WAPI Stack
├── wapi_app (Go + Gin)
│   ├── Conecta: PostgreSQL global
│   ├── Conecta: Redis global
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

- ⚠️ **Configure o Cloudflare** em modo SSL/TLS "Full" (não "Flexible")
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
- [SetupOrion](https://github.com/oriondesign2015/SetupOrion) - Infraestrutura recomendada

## 📞 Suporte

Para reportar bugs ou solicitar features, abra uma [issue](https://github.com/br6unoc/wapi/issues).

---

Desenvolvido com ❤️ por [br6unoc](https://github.com/br6unoc)
