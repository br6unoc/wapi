#!/bin/bash

echo ""
echo "╔══════════════════════════════════════╗"
echo "║       WAPI — Instalação              ║"
echo "║    WhatsApp API Manager              ║"
echo "╚══════════════════════════════════════╝"
echo ""

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}Vamos configurar o sistema. Pressione ENTER para aceitar o valor padrão.${NC}"
echo ""

read -p "URL do sistema (ex: https://wapi.seudominio.com): " APP_URL
APP_URL=${APP_URL:-http://localhost:8080}

read -p "Usuário admin [admin]: " ADMIN_USER
ADMIN_USER=${ADMIN_USER:-admin}

read -s -p "Senha do admin: " ADMIN_PASSWORD
echo ""
if [ -z "$ADMIN_PASSWORD" ]; then
  ADMIN_PASSWORD=$(openssl rand -hex 12)
  echo -e "${GREEN}Senha gerada automaticamente: $ADMIN_PASSWORD${NC}"
fi

read -p "Senha do PostgreSQL [gerada automaticamente]: " POSTGRES_PASSWORD
if [ -z "$POSTGRES_PASSWORD" ]; then
  POSTGRES_PASSWORD=$(openssl rand -hex 16)
fi

JWT_SECRET=$(openssl rand -hex 32)

echo ""
echo -e "${GREEN}Gerando arquivo .env...${NC}"

cat > .env << ENVEOF
PORT=8080
APP_URL=${APP_URL}
POSTGRES_HOST=postgres
POSTGRES_PORT=5432
POSTGRES_USER=wapi
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=wapi
REDIS_HOST=redis
REDIS_PORT=6379
WHISPER_URL=http://whisper:9000
JWT_SECRET=${JWT_SECRET}
ADMIN_USER=${ADMIN_USER}
ADMIN_PASSWORD=${ADMIN_PASSWORD}
ENVEOF

echo -e "${GREEN}Arquivo .env criado!${NC}"
echo ""

sed -i "s|http://localhost:8080|${APP_URL}|g" web/index.html
echo -e "${GREEN}Frontend atualizado com a URL: ${APP_URL}${NC}"
echo ""

if ! command -v docker &> /dev/null; then
  echo -e "${YELLOW}Docker não encontrado. Instalando...${NC}"
  curl -fsSL https://get.docker.com | sh
fi

if ! command -v docker compose &> /dev/null; then
  echo -e "${YELLOW}Docker Compose não encontrado. Instalando...${NC}"
  apt-get install -y docker-compose-plugin
fi

echo -e "${GREEN}Iniciando containers...${NC}"
echo ""
docker compose up -d --build

echo ""
echo "╔══════════════════════════════════════╗"
echo "║         Instalação Concluída!        ║"
echo "╚══════════════════════════════════════╝"
echo ""
echo -e "URL: ${GREEN}${APP_URL}${NC}"
echo -e "Usuário: ${GREEN}${ADMIN_USER}${NC}"
echo -e "Senha: ${GREEN}${ADMIN_PASSWORD}${NC}"
echo ""
echo -e "${YELLOW}Guarde essas informações em local seguro!${NC}"
echo ""
