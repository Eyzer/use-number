#!/bin/bash
# 发文字号取号系统 一键安装脚本（麒麟 ARM64 / 通用 Linux）
# 需 root 权限运行

set -e

if [ "$EUID" -ne 0 ]; then
  echo "请使用 root 权限运行: sudo bash install.sh [端口号]"
  exit 1
fi

PORT=${1:-80}
INSTALL_DIR="/opt/use-number"
SERVICE="use-number"

echo "=========================================="
echo "  发文字号取号系统 安装"
echo "  端口: $PORT  目录: $INSTALL_DIR"
echo "=========================================="

if ss -ltn 2>/dev/null | grep -q ":$PORT "; then
  echo "端口 $PORT 已被占用，请换端口: sudo bash install.sh 8080"
  exit 1
fi

BIN=""
for p in "use-number-kylin-arm64" "dist/use-number-kylin-arm64"; do
  if [ -f "$p" ]; then BIN="$p"; break; fi
done
if [ -z "$BIN" ]; then
  echo "缺少 use-number-kylin-arm64，请与本脚本放在同一目录（或 dist/ 下）"
  exit 1
fi

systemctl stop $SERVICE 2>/dev/null || true

mkdir -p "$INSTALL_DIR/data"
cp "$BIN" "$INSTALL_DIR/use-number"
chmod +x "$INSTALL_DIR/use-number"

cat > /etc/systemd/system/$SERVICE.service <<EOF
[Unit]
Description=发文字号取号系统
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/use-number -port $PORT
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable $SERVICE >/dev/null 2>&1
systemctl restart $SERVICE
sleep 2

if systemctl is-active --quiet $SERVICE; then
  IP=$(ip addr show 2>/dev/null | grep 'inet ' | grep -v '127.0.0.1' | awk '{print $2}' | cut -d/ -f1 | head -n1)
  echo ""
  echo "安装成功"
  echo "  本机访问: http://127.0.0.1:$PORT"
  [ -n "$IP" ] && echo "  局域网访问: http://$IP:$PORT"
  echo "  前台取号: http://$IP:$PORT/"
  echo "  后台管理: http://$IP:$PORT/admin"
  echo "  默认管理员: admin / admin123"
  echo ""
  echo "  数据目录: $INSTALL_DIR/data"
  echo "  常用命令: systemctl status|restart|stop $SERVICE"
else
  echo "启动失败，日志如下："
  journalctl -u $SERVICE -n 30 --no-pager
  exit 1
fi
