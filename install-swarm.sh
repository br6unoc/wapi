#!/bin/bash

echo ""
echo "╔════════════════════════════════════════╗"
echo "║       WAPI — Instalação Swarm          ║"
echo "║      WhatsApp API Manager              ║"
echo "╚════════════════════════════════════════╝"
echo ""

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Vamos configurar o sistema para Docker Swarm.${NC}"
echo ""

# Verificar se está no Swarm
if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
  echo -e "${YELLOW}Docker Swarm não está ativo. Deseja inicializar? (s/n)${NC}"
  read -p "> " INIT_SWARM
  if [ "$INIT_SWARM" = "s" ]; then
    docker swarm init
  else
    echo "Instalação cancelada. Inicialize o Swarm primeiro com: docker swarm init"
    exit 1
  fi
fi

# Domínio
read -p "Domínio do sistema (ex: wapi.seudominio.com): " DOMAIN
DOMAIN=${DOMAIN:-wapi.localhost}

# Admin
read -p "Usuário admin [admin]: " ADMIN_USER
ADMIN_USER=${ADMIN_USER:-admin}

read -s -p "Senha do admin: " ADMIN_PASSWORD
echo ""
if [ -z "$ADMIN_PASSWORD" ]; then
  ADMIN_PASSWORD=$(openssl rand -hex 12)
  echo -e "${GREEN}Senha gerada: $ADMIN_PASSWORD${NC}"
fi

# Senhas automáticas
POSTGRES_PASSWORD=$(openssl rand -hex 16)
JWT_SECRET=$(openssl rand -hex 32)

# Nome da rede Traefik
read -p "Nome da rede do Traefik [traefik-public]: " TRAEFIK_NETWORK
TRAEFIK_NETWORK=${TRAEFIK_NETWORK:-traefik-public}

# Cert resolver
read -p "Nome do certresolver do Traefik [letsencrypt]: " CERT_RESOLVER
CERT_RESOLVER=${CERT_RESOLVER:-letsencrypt}

# Criar rede se não existir
if ! docker network inspect $TRAEFIK_NETWORK &>/dev/null; then
  echo -e "${YELLOW}Criando rede $TRAEFIK_NETWORK...${NC}"
  docker network create --driver overlay $TRAEFIK_NETWORK
fi

echo ""
echo -e "${GREEN}Gerando docker-stack.yml...${NC}"

cat > docker-stack.yml << STACKEOF
version: '3.8'

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: wapi
      POSTGRES_PASSWORD: $POSTGRES_PASSWORD
      POSTGRES_DB: wapi
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - internal
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure

  redis:
    image: redis:7-alpine
    networks:
      - internal
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure

  whisper:
    image: br6unoc/wapi-whisper:latest
    networks:
      - internal
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure

  app:
    image: br6unoc/wapi:latest
    environment:
      PORT: 8080
      POSTGRES_HOST: postgres
      POSTGRES_PORT: 5432
      POSTGRES_USER: wapi
      POSTGRES_PASSWORD: $POSTGRES_PASSWORD
      POSTGRES_DB: wapi
      REDIS_HOST: redis
      REDIS_PORT: 6379
      WHISPER_URL: http://whisper:9000
      JWT_SECRET: $JWT_SECRET
      ADMIN_USER: $ADMIN_USER
      ADMIN_PASSWORD: $ADMIN_PASSWORD
    volumes:
      - sessions_data:/app/sessions
    networks:
      - internal
      - $TRAEFIK_NETWORK
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure
      labels:
        - traefik.enable=true
        - traefik.docker.network=$TRAEFIK_NETWORK
        - traefik.http.routers.wapi.rule=Host(\`$DOMAIN\`)
        - traefik.http.routers.wapi.entrypoints=websecure
        - traefik.http.routers.wapi.tls=true
        - traefik.http.routers.wapi.tls.certresolver=$CERT_RESOLVER
        - traefik.http.services.wapi.loadbalancer.server.port=8080

volumes:
  postgres_data:
  sessions_data:

networks:
  internal:
    driver: overlay
  $TRAEFIK_NETWORK:
    external: true
STACKEOF

echo -e "${GREEN}Arquivo docker-stack.yml criado!${NC}"
echo ""

echo -e "${GREEN}Fazendo deploy da stack...${NC}"
docker stack deploy -c docker-stack.yml wapi

echo ""
echo "╔════════════════════════════════════════╗"
echo "║       Instalação Concluída!            ║"
echo "╚════════════════════════════════════════╝"
echo ""
echo -e "URL: ${GREEN}https://$DOMAIN${NC}"
echo -e "Usuário: ${GREEN}$ADMIN_USER${NC}"
echo -e "Senha: ${GREEN}$ADMIN_PASSWORD${NC}"
echo ""
echo -e "${YELLOW}Aguarde ~30 segundos para os containers subirem.${NC}"
echo -e "${YELLOW}Verifique o status: docker service ls | grep wapi${NC}"
echo ""
