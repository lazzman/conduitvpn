#!/usr/bin/env bash
# ConduitVPN 一键部署（Docker 版，~50 行，对比 Python 原版 1184 行）
set -euo pipefail

IMAGE="conduitvpn:latest"
NAME="conduitvpn"
UI_PORT="${UI_PORT:-8787}"
PROXY_PORT="${PROXY_PORT:-7928}"
DATA_DIR="${DATA_DIR:-/data/conduitvpn}"
PROXY_HOST="${PROXY_HOST:-127.0.0.1}"   # 代理端口默认只绑本机回环
UI_HOST="${UI_HOST:-0.0.0.0}"           # 管理后台可公网访问（有随机后缀保护）
HY2_PORT="${HY2_PORT:-0}"              # hysteria2 入站端口（0=关闭）
HY2_PASSWORD="${HY2_PASSWORD:-}"
HY2_OBFS_PASSWORD="${HY2_OBFS_PASSWORD:-}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "[错误] 缺少 $1，请先安装"; exit 1; }; }
need docker

if [ "$HY2_PORT" != "0" ] && [ -z "$HY2_PASSWORD" ]; then
    echo "[错误] 启用 hy2 必须设置 HY2_PASSWORD"
    exit 1
fi

echo "→ 构建镜像 $IMAGE ..."
docker build -t "$IMAGE" .

echo "→ 创建数据目录 $DATA_DIR ..."
mkdir -p "$DATA_DIR"

echo "→ 启动容器（TUN + NET_ADMIN）..."
docker rm -f "$NAME" >/dev/null 2>&1 || true

PORT_ARGS="-p $UI_HOST:$UI_PORT:8787 -p $PROXY_HOST:$PROXY_PORT:7928"
ENV_ARGS="-e LOCAL_PROXY_PORT=7928"
if [ "$HY2_PORT" != "0" ]; then
    PORT_ARGS="$PORT_ARGS -p $HY2_PORT:$HY2_PORT/udp"
    ENV_ARGS="$ENV_ARGS -e HY2_PORT=$HY2_PORT -e HY2_PASSWORD=$HY2_PASSWORD"
    [ -n "$HY2_OBFS_PASSWORD" ] && ENV_ARGS="$ENV_ARGS -e HY2_OBFS_PASSWORD=$HY2_OBFS_PASSWORD"
fi

docker run -d \
  --name "$NAME" \
  --restart unless-stopped \
  --cap-add=NET_ADMIN \
  --device=/dev/net/tun \
  -v "$DATA_DIR:/data/conduitvpn" \
  $PORT_ARGS $ENV_ARGS \
  "$IMAGE" >/dev/null

echo
echo "✅ 部署完成！"
echo "   后台地址: http://<你的IP>:$UI_PORT/  (将自动跳转到带安全后缀的 URL)"
echo "   代理端口: $PROXY_HOST:$PROXY_PORT (HTTP + SOCKS5 双协议)"
if [ "$HY2_PORT" != "0" ]; then
    echo "   hy2 入站: 0.0.0.0:$HY2_PORT/udp (hysteria2, 需 HY2_PASSWORD)"
fi
echo
echo "   常用命令:"
echo "     docker logs -f $NAME     # 查看日志"
echo "     docker restart $NAME     # 重启"
echo "     docker rm -f $NAME       # 卸载"
