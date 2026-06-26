# Leo-One RMM — Backend Go
# Build context : ./backend

# ── Stage 1 : compilation ───────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Télécharge les dépendances en premier (cache Docker)
COPY go.mod go.sum ./
RUN go mod download

# Copie le reste du code source
COPY . .

# Compilation statique (sans CGO pour l'image alpine finale)
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /app/server ./cmd/server

# ── Stage 2 : image minimale ────────────────────────────────────
FROM alpine:3.19

# Certificats TLS (requis pour les appels HTTPS sortants)
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/server /app/server

EXPOSE 8080
EXPOSE 8081

CMD ["/app/server"]
