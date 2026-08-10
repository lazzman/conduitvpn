#!/usr/bin/env bash
# ConduitVPN 一键部署（Docker 版，~50 行，对比 Python 原版 1184 行）
set -euo pipefail

IMAGE="ghcr.io/sarices/conduitvpn:latest"  # CI 推送即构建
BUILD_LOCAL="${BUILD_LOCAL:-0}"            # 1=本地 docker build（不拉 GHCR）
NAME="conduitvpn"
UI_PORT="${UI_PORT:-8787}"
PROXY_PORT="${PROXY_PORT:-7928}"
DATA_DIR="${DATA_DIR:-/data/conduitvpn}"
PROXY_HOST="${PROXY_HOST:-127.0.0.1}"
UI_BIND_HOST="${UI_BIND_HOST:-${UI_HOST:-0.0.0.0}}"
HY2_PORT="${HY2_PORT:-0}"              # hysteria2 入站端口（0=关闭）
HY2_PASSWORD="${HY2_PASSWORD:-}"
HY2_OBFS_PASSWORD="${HY2_OBFS_PASSWORD:-}"
UI_USER="${UI_USER:-}"
UI_PASSWORD="${UI_PASSWORD:-}"
UI_TLS_CERT="${UI_TLS_CERT:-}"
UI_TLS_KEY="${UI_TLS_KEY:-}"
PROXY_USER="${LOCAL_PROXY_USER:-}"
PROXY_PASS="${LOCAL_PROXY_PASS:-}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "[错误] 缺少 $1，请先安装"; exit 1; }; }
need docker

if [ "$HY2_PORT" != "0" ] && [ -z "$HY2_PASSWORD" ]; then
    echo "[错误] 启用 hy2 必须设置 HY2_PASSWORD"
    exit 1
fi
if [ "$HY2_PORT" != "0" ] && [ "${#HY2_PASSWORD}" -lt 16 ]; then
    echo "[错误] HY2_PASSWORD 至少需要 16 个字符"
    exit 1
fi
if [ -z "$UI_USER" ] || [ "${#UI_PASSWORD}" -lt 16 ]; then
    echo "[错误] 生产部署必须设置 UI_USER 和至少 16 个字符的 UI_PASSWORD"
    exit 1
fi
if [ ! -f "$UI_TLS_CERT" ] || [ ! -f "$UI_TLS_KEY" ]; then
    echo "[错误] 必须设置存在的 UI_TLS_CERT 和 UI_TLS_KEY 文件"
    exit 1
fi
if [ -z "$PROXY_USER" ] || [ "${#PROXY_PASS}" -lt 16 ]; then
    echo "[错误] Docker 代理必须设置 LOCAL_PROXY_USER 和至少 16 个字符的 LOCAL_PROXY_PASS"
    exit 1
fi

if [ "$BUILD_LOCAL" = "1" ]; then
    echo "→ 本地构建镜像 $IMAGE ..."
    docker build -t conduitvpn:local .
    IMAGE="conduitvpn:local"
else
    echo "→ 拉取镜像 $IMAGE ..."
    docker pull "$IMAGE"
fi

echo "→ 创建数据目录 $DATA_DIR ..."
mkdir -p "$DATA_DIR"
chmod 700 "$DATA_DIR"

echo "→ 启动容器（TUN + NET_ADMIN）..."
docker rm -f "$NAME" >/dev/null 2>&1 || true

PORT_ARGS=( -p "$UI_BIND_HOST:$UI_PORT:8787" -p "$PROXY_HOST:$PROXY_PORT:7928" )
ENV_ARGS=(
    -e UI_HOST=0.0.0.0
    -e UI_USER="$UI_USER"
    -e UI_PASSWORD="$UI_PASSWORD"
    -e UI_TLS_CERT=/run/conduitvpn-tls/cert.pem
    -e UI_TLS_KEY=/run/conduitvpn-tls/key.pem
    -e LOCAL_PROXY_HOST=0.0.0.0
    -e LOCAL_PROXY_PORT=7928
    -e LOCAL_PROXY_USER="$PROXY_USER"
    -e LOCAL_PROXY_PASS="$PROXY_PASS"
)
MOUNT_ARGS=(
    -v "$DATA_DIR:/data/conduitvpn"
    -v "$UI_TLS_CERT:/run/conduitvpn-tls/cert.pem:ro"
    -v "$UI_TLS_KEY:/run/conduitvpn-tls/key.pem:ro"
)
if [ "$HY2_PORT" != "0" ]; then
    PORT_ARGS+=( -p "$HY2_PORT:$HY2_PORT/udp" )
    ENV_ARGS+=( -e HY2_PORT="$HY2_PORT" -e HY2_PASSWORD="$HY2_PASSWORD" )
    [ -n "$HY2_OBFS_PASSWORD" ] && ENV_ARGS+=( -e HY2_OBFS_PASSWORD="$HY2_OBFS_PASSWORD" )
fi

docker run -d \
  --name "$NAME" \
  --restart unless-stopped \
  --cap-drop=ALL \
  --cap-add=NET_ADMIN \
  --security-opt=no-new-privileges \
  --read-only \
  --tmpfs /tmp \
  --device=/dev/net/tun \
  "${MOUNT_ARGS[@]}" \
  "${PORT_ARGS[@]}" "${ENV_ARGS[@]}" \
  "$IMAGE" >/dev/null

echo
echo "✅ 部署完成！"
echo "   后台地址: https://<你的IP>:$UI_PORT/  (需登录)"
echo "   代理端口: $PROXY_HOST:$PROXY_PORT (HTTP + SOCKS5 双协议)"
if [ "$HY2_PORT" != "0" ]; then
    echo "   hy2 入站: 0.0.0.0:$HY2_PORT/udp (hysteria2, 需 HY2_PASSWORD)"
fi
echo
echo "   常用命令:"
echo "     docker logs -f $NAME     # 查看日志"
echo "     docker restart $NAME     # 重启"
echo "     docker rm -f $NAME       # 卸载"
