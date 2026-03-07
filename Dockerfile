FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o bot cmd/bot/main.go

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/bot .

COPY --from=builder /app/config ./config

CMD [ "./bot" ]