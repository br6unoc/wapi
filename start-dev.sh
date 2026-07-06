#!/bin/bash
# Script para (re)iniciar o container de desenvolvimento do Botwapp
# Garante que ffmpeg e dependências estejam instalados

CONTAINER=wapi-dev

# Instala ffmpeg se não estiver presente
docker exec $CONTAINER sh -c "which ffmpeg || apk add --no-cache ffmpeg" 2>/dev/null

# Mata processo antigo do server se existir
docker exec $CONTAINER sh -c "pkill -f './server' 2>/dev/null; true"
sleep 1

# Inicia o server em background
docker exec -d $CONTAINER sh -c "cd /app && nohup ./server >> /var/log/botwapp.log 2>&1"
sleep 2

# Verifica
if docker exec $CONTAINER sh -c "pgrep -f './server'" > /dev/null 2>&1; then
  echo "✅ Botwapp dev server iniciado com sucesso"
else
  echo "❌ Falha ao iniciar o server"
fi
