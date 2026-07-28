FROM golang:1.24-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o worker-app ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -o producer-app ./cmd/producer

FROM alpine:latest
WORKDIR /app

COPY --from=builder /app/worker-app .
COPY --from=builder /app/producer-app .
RUN chmod +x worker-app producer-app
