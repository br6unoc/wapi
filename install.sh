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

if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
  echo -e "${RED}❌ Docker Swarm não está ativo!${NC}"
  exit 1
fi

echo -e "${YELLOW}Verificando pré-requisitos...${NC}"
echo ""

if ! docker service ls 2>/dev/null | grep -q "postgres"; then
  echo -e "${RED}❌ PostgreSQL não encontrado!${NC}"
  exit 1
fi
echo -e "${GREEN}✅ PostgreSQL${NC}"

if ! docker service ls 2>/dev/null | grep -q "redis"; then
  echo -e "${RED}❌ Redis não encontrado!${NC}"
  exit 1
fi
echo -e "${GREEN}✅ Redis${NC}"

if ! docker service ls 2>/dev/null | grep -q "traefik"; then
  echo -e "${RED}❌ Traefik não encontrado!${NC}"
  exit 1
fi
echo -e "${GREEN}✅ Traefik${NC}"

echo ""

TRAEFIK_NETWORK=$(docker service inspect $(docker service ls --filter name=traefik --format "{{.ID}}") --format='{{range .Spec.TaskTemplate.Networks}}{{.Target}}{{end}}' 2>/dev/null | head -1)
if [ -n "$TRAEFIK_NETWORK" ]; then
  TRAEFIK_NETWORK_NAME=$(docker network inspect $TRAEFIK_NETWORK --format='{{.Name}}' 2>/dev/null)
  echo -e "${GREEN}Rede: $TRAEFIK_NETWORK_NAME${NC}"
else
  TRAEFIK_NETWORK_NAME="traefik-public"
fi

CERT_RESOLVER=$(docker service inspect $(docker service ls --filter name=traefik --format "{{.ID}}") --format='{{json .Spec.TaskTemplate.ContainerSpec.Args}}' 2>/dev/null | grep -oP 'certificatesresolvers\.\K[^.]+' | head -1)
if [ -z "$CERT_RESOLVER" ]; then
  CERT_RESOLVER="letsencrypt"
fi
echo -e "${GREEN}CertResolver: $CERT_RESOLVER${NC}"

POSTGRES_HOST=$(docker service ps $(docker service ls --filter name=postgres --format "{{.Name}}") --format "{{.Name}}" 2>/dev/null | head -1 | cut -d'.' -f1)
[ -z "$POSTGRES_HOST" ] && POSTGRES_HOST="postgres"

REDIS_HOST=$(docker service ps $(docker service ls --filter name=redis --format "{{.Name}}") --format "{{.Name}}" 2>/dev/null | head -1 | cut -d'.' -f1)
[ -z "$REDIS_HOST" ] && REDIS_HOST="redis"

echo ""
read -p "Domínio (ex: wapi.seudominio.com): " DOMAIN
while [ -z "$DOMAIN" ]; do
  read -p "Domínio é obrigatório: " DOMAIN
done

read -p "Usuário admin [admin]: " ADMIN_USER
ADMIN_USER=${ADMIN_USER:-admin}

read -s -p "Senha admin (vazio=gerar): " ADMIN_PASSWORD
echo ""
[ -z "$ADMIN_PASSWORD" ] && ADMIN_PASSWORD=$(openssl rand -hex 12) && echo -e "${GREEN}Senha: $ADMIN_PASSWORD${NC}"

POSTGRES_PASSWORD=$(openssl rand -hex 16)
JWT_SECRET=$(openssl rand -hex 32)

echo ""
echo -e "${GREEN}Configurando PostgreSQL...${NC}"

POSTGRES_CONTAINER=$(docker ps --filter name=postgres --format "{{.Names}}" | head -1)
if [ -n "$POSTGRES_CONTAINER" ]; then
  docker exec $POSTGRES_CONTAINER psql -U postgres -c "CREATE DATABASE wapi;" 2>/dev/null
  docker exec $POSTGRES_CONTAINER psql -U postgres -c "DO \$\$ BEGIN IF NOT EXISTS (SELECT FROM pg_user WHERE usename = 'wapi') THEN CREATE USER wapi WITH PASSWORD '$POSTGRES_PASSWORD'; ELSE ALTER USER wapi WITH PASSWORD '$POSTGRES_PASSWORD'; END IF; END \$\$;" 2>/dev/null
  docker exec $POSTGRES_CONTAINER psql -U postgres -d wapi -c "GRANT ALL PRIVILEGES ON DATABASE wapi TO wapi;" 2>/dev/null
  docker exec $POSTGRES_CONTAINER psql -U postgres -d wapi -c "ALTER DATABASE wapi OWNER TO wapi;" 2>/dev/null
  docker exec $POSTGRES_CONTAINER psql -U postgres -d wapi -c "GRANT ALL ON SCHEMA public TO wapi;" 2>/dev/null
  echo -e "${GREEN}✅ Database configurado${NC}"
fi

echo ""
echo -e "${GREEN}Criando stack...${NC}"

cat > docker-stack.yml << STACKEOF
version: '3.8'
services:
  whisper:
    image: br6unoc/wapi-whisper:latest
    networks:
      - $TRAEFIK_NETWORK_NAME
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure
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
      - $TRAEFIK_NETWORK_NAME
    deploy:
      replicas: 1
      restart_policy:
        condition: on-failure
      labels:
        - traefik.enable=1
        - traefik.http.routers.wapi.entrypoints=websecure
        - traefik.http.routers.wapi.priority=1
        - traefik.http.routers.wapi.rule=Host(\`$DOMAIN\`)
        - traefik.http.routers.wapi.service=wapi
        - traefik.http.routers.wapi.tls.certresolver=$CERT_RESOLVER
        - traefik.http.services.wapi.loadbalancer.passHostHeader=true
        - traefik.http.services.wapi.loadbalancer.server.port=8080
volumes:
  sessions_data:
networks:
  $TRAEFIK_NETWORK_NAME:
    external: true
STACKEOF

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
