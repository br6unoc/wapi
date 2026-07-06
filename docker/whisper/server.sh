#!/bin/bash
./build/bin/whisper-server \
  --model /app/models/ggml-base.bin \
  --host 0.0.0.0 \
  --port 9000 \
  --language pt
