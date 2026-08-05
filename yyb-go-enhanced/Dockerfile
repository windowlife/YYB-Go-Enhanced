FROM golang:1.23-alpine AS builder

RUN apk add --no-cache \
      git \
      ca-certificates

WORKDIR /src

ENV GOPROXY=https://goproxy.cn,direct

# 先复制依赖文件，提高后续构建缓存命中率
COPY src/go.mod src/go.sum ./

RUN go mod download

# 直接使用 HAOS 已经下载好的本地源码
COPY src/ ./

RUN CGO_ENABLED=0 \
    GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/yyb-go \
      ./cmd/yyb-go


FROM ghcr.io/home-assistant/base:latest

ARG BUILD_VERSION
ARG BUILD_ARCH

RUN apk add --no-cache \
      nginx \
      ca-certificates \
      tzdata \
      su-exec \
    && addgroup -S yyb \
    && adduser -S -G yyb -h /app yyb \
    && mkdir -p \
         /app \
         /run/nginx \
         /etc/nginx/yyb

WORKDIR /app

COPY --from=builder /out/yyb-go /app/yyb-go
COPY --from=builder /src/resource /app/resource-default

COPY run.sh /run.sh
COPY ingress.conf /etc/nginx/http.d/default.conf
COPY ingress.js /etc/nginx/yyb/ingress.js

RUN chmod 755 \
      /run.sh \
      /app/yyb-go \
    && chmod 644 \
      /etc/nginx/http.d/default.conf \
      /etc/nginx/yyb/ingress.js \
    && chown -R yyb:yyb /app

EXPOSE 8000
EXPOSE 8099

LABEL \
  io.hass.version="${BUILD_VERSION}" \
  io.hass.type="app" \
  io.hass.arch="${BUILD_ARCH}"

CMD ["/run.sh"]