#!/usr/bin/env bash
# AimiliVPN 一键部署（Docker 版，~50 行，对比 Python 原版 1184 行）
set -euo pipefail

IMAGE="aimilivpn:latest"
NAME="aimilivpn"
UI_PORT="${UI_PORT:-8787}"
PROXY_PORT="${PROXY_PORT:-7928}"
DATA_DIR="${DATA_DIR:-/data/aimilivpn}"
PROXY_HOST="${PROXY_HOST:-127.0.0.1}"   # 代理端口默认只绑本机回环
UI_HOST="${UI_HOST:-0.0.0.0}"           # 管理后台可公网访问（有随机后缀保护）

need() { command -v "$1" >/dev/null 2>&1 || { echo "[错误] 缺少 $1，请先安装"; exit 1; }; }
need docker

echo "→ 构建镜像 $IMAGE ..."
docker build -t "$IMAGE" .

echo "→ 创建数据目录 $DATA_DIR ..."
mkdir -p "$DATA_DIR"

echo "→ 启动容器（TUN + NET_ADMIN）..."
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d \
  --name "$NAME" \
  --restart unless-stopped \
  --cap-add=NET_ADMIN \
  --device=/dev/net/tun \
  -v "$DATA_DIR:/data/aimilivpn" \
  -p "$UI_HOST:$UI_PORT:8787" \
  -p "$PROXY_HOST:$PROXY_PORT:7928" \
  -e LOCAL_PROXY_PORT=7928 \
  "$IMAGE" >/dev/null

echo
echo "✅ 部署完成！"
echo "   后台地址: http://<你的IP>:$UI_PORT/  (将自动跳转到带安全后缀的 URL)"
echo "   代理端口: $PROXY_HOST:$PROXY_PORT (HTTP + SOCKS5 双协议)"
echo
echo "   常用命令:"
echo "     docker logs -f $NAME     # 查看日志"
echo "     docker restart $NAME     # 重启"
echo "     docker rm -f $NAME       # 卸载"
