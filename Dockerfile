FROM golang:1.24-alpine AS builder
WORKDIR /app
RUN apk add --no-cache gcc musl-dev
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o wapi cmd/server/main.go

FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates ffmpeg
COPY --from=builder /app/wapi .
COPY web/ ./web/
EXPOSE 8080
CMD ["./wapi"]
