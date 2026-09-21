FROM golang:1.24-alpine AS builder

WORKDIR /src

# 依存解決（ソースコードより先にコピーしてレイヤーキャッシュを活かす）
COPY go-api/go.mod go-api/go.sum ./
RUN go mod download

# アプリケーションコードをコピーしてビルド
COPY go-api/ ./
RUN CGO_ENABLED=0 go build -o /bin/api ./cmd/api

FROM alpine:3.20

# Yahoo Finance / Claude API / Slack / yanoshin.jp への外部HTTPS通信にCA証明書が必要
RUN apk add --no-cache ca-certificates

COPY --from=builder /bin/api /bin/api

RUN adduser -D -u 10001 app
USER app

EXPOSE 8080

CMD ["/bin/api"]
