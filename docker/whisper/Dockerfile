FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    build-essential \
    cmake \
    git \
    ffmpeg \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

RUN git clone https://github.com/ggerganov/whisper.cpp.git .

RUN cmake -B build && cmake --build build --config Release

RUN mkdir -p /models

RUN bash ./models/download-ggml-model.sh base

COPY server.sh /app/server.sh
RUN chmod +x /app/server.sh

EXPOSE 9000

CMD ["/app/server.sh"]
