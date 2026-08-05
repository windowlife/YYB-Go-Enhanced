FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY src/ ./

RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" \
    -o /yyb-go ./cmd/yyb-go


FROM ghcr.io/home-assistant/base:3.23

RUN apk add --no-cache ca-certificates

COPY --from=builder /yyb-go /app/yyb-go
COPY --from=builder /src/resource /app/resource-default
COPY run.sh /run.sh

RUN chmod +x /run.sh

CMD ["/run.sh"]