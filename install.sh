#!/bin/bash

echo ""
echo "╔════════════════════════════════════════╗"
echo "║       WAPI — Instalação Swarm          ║"
echo "║      WhatsApp API Manager              ║"
echo "╚════════════════════════════════════════╝"
echo ""

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Verificar se está no Swarm
if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
  echo -e "${RED}❌ Docker Swarm não está ativo!${NC}"
  echo "Inicialize o Swarm primeiro com: docker swarm init"
  exit 1
fi

echo -e "${YELLOW}Verificando pré-requisitos...${NC}"
echo ""

# Verificar Postgres
if ! docker service ls 2>/dev/null | grep -q "postgres"; then
  echo -e "${RED}❌ PostgreSQL global não encontrado!${NC}"
  echo "Instale o PostgreSQL usando o SetupOrion primeiro."
  exit 1
fi
echo -e "${GREEN}✅ PostgreSQL encontrado${NC}"

# Verificar Redis
if ! docker service ls 2>/dev/null | grep -q "redis"; then
  echo -e "${RED}❌ Redis global não encontrado!${NC}"
  echo "Instale o Redis usando o SetupOrion primeiro."
  exit 1
fi
echo -e "${GREEN}✅ Redis encontrado${NC}"

# Verificar Traefik
if ! docker service ls 2>/dev/null | grep -q "traefik"; then
  echo -e "${RED}❌ Traefik não encontrado!${NC}"
  echo "Instale o Traefik usando o SetupOrion primeiro."
  exit 1
fi
echo -e "${GREEN}✅ Traefik encontrado${NC}"

echo ""
echo -e "${GREEN}Todos os pré-requisitos atendidos!${NC}"
echo ""
echo -e "${YELLOW}Vamos configurar o WAPI para Docker Swarm.${NC}"
echo ""

# Detectar rede do Traefik automaticamente
TRAEFIK_NETWORK=$(docker service inspect $(docker service ls --filter name=traefik --format "{{.ID}}") --format='{{range .Spec.TaskTemplate.Networks}}{{.Target}}{{end}}' 2>/dev/null | head -1)
if [ -n "$TRAEFIK_NETWORK" ]; then
  TRAEFIK_NETWORK_NAME=$(docker network inspect $TRAEFIK_NETWORK --format='{{.Name}}' 2>/dev/null)
  echo -e "${GREEN}Rede do Traefik detectada: $TRAEFIK_NETWORK_NAME${NC}"
else
  read -p "Nome da rede do Traefik [traefik-public]: " TRAEFIK_NETWORK_NAME
  TRAEFIK_NETWORK_NAME=${TRAEFIK_NETWORK_NAME:-traefik-public}
fi

# Detectar certresolver do Traefik
CERT_RESOLVER=$(docker service inspect $(docker service ls --filter name=traefik --format "{{.ID}}") --format='{{json .Spec.TaskTemplate.ContainerSpec.Args}}' 2>/dev/null | grep -oP 'certificatesresolvers\.\K[^.]+' | head -1)
if [ -n "$CERT_RESOLVER" ]; then
  echo -e "${GREEN}CertResolver detectado: $CERT_RESOLVER${NC}"
else
  read -p "Nome do certresolver do Traefik [letsencrypt]: " CERT_RESOLVER
  CERT_RESOLVER=${CERT_RESOLVER:-letsencrypt}
fi

echo ""

# Detectar host do Postgres
POSTGRES_HOST=$(docker service ps $(docker service ls --filter name=postgres --format "{{.Name}}") --format "{{.Name}}" 2>/dev/null | head -1 | cut -d'.' -f1)
if [ -z "$POSTGRES_HOST" ]; then
  POSTGRES_HOST="postgres"
fi
echo -e "${GREEN}Postgres host: $POSTGRES_HOST${NC}"

# Detectar host do Redis
REDIS_HOST=$(docker service ps $(docker service ls --filter name=redis --format "{{.Name}}") --format "{{.Name}}" 2>/dev/null | head -1 | cut -d'.' -f1)
if [ -z "$REDIS_HOST" ]; then
  REDIS_HOST="redis"
fi
echo -e "${GREEN}Redis host: $REDIS_HOST${NC}"

echo ""

# Domínio
read -p "Domínio do sistema (ex: wapi.seudominio.com): " DOMAIN
while [ -z "$DOMAIN" ]; do
  echo -e "${RED}Domínio é obrigatório!${NC}"
  read -p "Domínio do sistema: " DOMAIN
done

# Admin
read -p "Usuário admin [admin]: " ADMIN_USER
ADMIN_USER=${ADMIN_USER:-admin}

read -s -p "Senha do admin (deixe vazio para gerar): " ADMIN_PASSWORD
echo ""
if [ -z "$ADMIN_PASSWORD" ]; then
  ADMIN_PASSWORD=$(openssl rand -hex 12)
  echo -e "${GREEN}Senha gerada: $ADMIN_PASSWORD${NC}"
fi

# Senhas automáticas
POSTGRES_PASSWORD=$(openssl rand -hex 16)
JWT_SECRET=$(openssl rand -hex 32)

# Verificar se a rede existe
if ! docker network inspect $TRAEFIK_NETWORK_NAME &>/dev/null; then
  echo -e "${YELLOW}Criando rede $TRAEFIK_NETWORK_NAME...${NC}"
  docker network create --driver overlay $TRAEFIK_NETWORK_NAME
fi

echo ""
echo -e "${GREEN}Criando database 'wapi' no PostgreSQL global...${NC}"

# Criar database no Postgres global (executa dentro do container)
POSTGRES_CONTAINER=$(docker ps --filter name=postgres --format "{{.Names}}" | head -1)
if [ -n "$POSTGRES_CONTAINER" ]; then
  docker exec $POSTGRES_CONTAINER psql -U postgres -c "CREATE DATABASE wapi;" 2>/dev/null || echo -e "${YELLOW}Database 'wapi' já existe ou erro ao criar (continuando...)${NC}"
  docker exec $POSTGRES_CONTAINER psql -U postgres -d wapi -c "CREATE USER wapi WITH PASSWORD '$POSTGRES_PASSWORD';" 2>/dev/null || true
  docker exec $POSTGRES_CONTAINER psql -U postgres -d wapi -c "GRANT ALL PRIVILEGES ON DATABASE wapi TO wapi;" 2>/dev/null || true
  docker exec $POSTGRES_CONTAINER psql -U postgres -d wapi -c "ALTER DATABASE wapi OWNER TO wapi;" 2>/dev/null || true
  echo -e "${GREEN}Database configurado!${NC}"
else
  echo -e "${YELLOW}Container do Postgres não encontrado. Tentando conexão por serviço...${NC}"
fi

echo ""
echo -e "${GREEN}Gerando docker-stack.yml...${NC}"

cat > docker-stack.yml << STACKEOF
version: '3.8'

services:
  whisper:
    image: br6unoc/wapi-whisper:latest
    networks:
      - internal
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3

  app:
    image: br6unoc/wapi:latest
    environment:
      PORT: 8080
      POSTGRES_HOST: $POSTGRES_HOST
      POSTGRES_PORT: 5432
      POSTGRES_USER: wapi
      POSTGRES_PASSWORD: $POSTGRES_PASSWORD
      POSTGRES_DB: wapi
      REDIS_HOST: $REDIS_HOST
      REDIS_PORT: 6379
      WHISPER_URL: http://whisper:9000
      JWT_SECRET: $JWT_SECRET
      ADMIN_USER: $ADMIN_USER
      ADMIN_PASSWORD: $ADMIN_PASSWORD
    volumes:
      - sessions_data:/app/sessions
    networks:
      - internal
      - $TRAEFIK_NETWORK_NAME
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure
        delay: 10s
        max_attempts: 5
      labels:
        - traefik.enable=true
        - traefik.docker.network=$TRAEFIK_NETWORK_NAME
        - traefik.http.routers.wapi.rule=Host(\`$DOMAIN\`)
        - traefik.http.routers.wapi.entrypoints=websecure
        - traefik.http.routers.wapi.tls=true
        - traefik.http.routers.wapi.tls.certresolver=$CERT_RESOLVER
        - traefik.http.services.wapi.loadbalancer.server.port=8080

volumes:
  sessions_data:

networks:
  internal:
    driver: overlay
  $TRAEFIK_NETWORK_NAME:
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
echo -e "${YELLOW}Aguarde ~40 segundos para os containers subirem.${NC}"
echo -e "${YELLOW}Verifique o status: docker service ls | grep wapi${NC}"
echo ""
echo -e "${GREEN}Guarde essas credenciais em local seguro!${NC}"
echo ""
