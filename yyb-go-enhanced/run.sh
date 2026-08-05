#!/usr/bin/with-contenv bashio

set -eu

DATA_ROOT="/data/resource"

YYB_PID=""
NGINX_PID=""

cleanup() {
    set +e

    if [ -n "${NGINX_PID}" ] && kill -0 "${NGINX_PID}" 2>/dev/null; then
        kill -TERM "${NGINX_PID}" 2>/dev/null
    fi

    if [ -n "${YYB_PID}" ] && kill -0 "${YYB_PID}" 2>/dev/null; then
        kill -TERM "${YYB_PID}" 2>/dev/null
    fi

    [ -n "${NGINX_PID}" ] && wait "${NGINX_PID}" 2>/dev/null
    [ -n "${YYB_PID}" ] && wait "${YYB_PID}" 2>/dev/null
}

trap 'cleanup; exit 0' TERM INT

bashio::log.info "正在准备 YYB Go Enhanced 数据目录……"

mkdir -p \
    "${DATA_ROOT}/db" \
    "${DATA_ROOT}/avatars" \
    "${DATA_ROOT}/qr"

# 模板属于程序资源，每次启动都更新为当前应用版本。
# 数据库、头像和二维码目录不会被覆盖。
rm -rf "${DATA_ROOT}/templates"
cp -a \
    "/app/resource-default/templates" \
    "${DATA_ROOT}/templates"

chown -R yyb:yyb "${DATA_ROOT}"

KEEPALIVE_INTERVAL="$(bashio::config 'keepalive_interval')"
KEEPALIVE_AHEAD="$(bashio::config 'keepalive_ahead')"

export QL_URL
export QL_CLIENT_ID
export QL_CLIENT_SECRET
export YYB_QINGLONG_SERVER
export YYB_QINGLONG_REPO

QL_URL="$(bashio::config 'ql_url')"
QL_CLIENT_ID="$(bashio::config 'ql_client_id')"
QL_CLIENT_SECRET="$(bashio::config 'ql_client_secret')"
YYB_QINGLONG_SERVER="$(bashio::config 'yyb_qinglong_server')"
YYB_QINGLONG_REPO="$(bashio::config 'yyb_qinglong_repo')"

bashio::log.info "数据目录：${DATA_ROOT}"
bashio::log.info "保活检查周期：${KEEPALIVE_INTERVAL}"
bashio::log.info "提前续期时间：${KEEPALIVE_AHEAD}"
bashio::log.info "Go API 监听：0.0.0.0:8000"
bashio::log.info "HAOS Ingress 监听：0.0.0.0:8099"

su-exec yyb:yyb \
    /app/yyb-go \
      -host 0.0.0.0 \
      -port 8000 \
      -resource-root "${DATA_ROOT}" \
      -keepalive-interval "${KEEPALIVE_INTERVAL}" \
      -keepalive-ahead "${KEEPALIVE_AHEAD}" &

YYB_PID="$!"

# 等待 Go 服务启动。
STARTED="false"

for _ in $(seq 1 30); do
    if ! kill -0 "${YYB_PID}" 2>/dev/null; then
        bashio::log.error "YYB Go 服务启动失败"
        wait "${YYB_PID}" || true
        exit 1
    fi

    if wget -qO- "http://127.0.0.1:8000/health" >/dev/null 2>&1; then
        STARTED="true"
        break
    fi

    sleep 1
done

if [ "${STARTED}" != "true" ]; then
    bashio::log.error "YYB Go 服务健康检查超时"
    cleanup
    exit 1
fi

nginx -t

nginx -g "daemon off;" &
NGINX_PID="$!"

bashio::log.info "YYB Go Enhanced 已启动"

# 任意一个进程退出，都停止整个应用，交给 Supervisor 重新启动。
while \
    kill -0 "${YYB_PID}" 2>/dev/null && \
    kill -0 "${NGINX_PID}" 2>/dev/null
do
    sleep 2
done

bashio::log.error "YYB Go 或 Nginx 进程意外退出"

cleanup
exit 1
