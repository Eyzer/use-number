#!/bin/bash
# 发文字号取号系统 卸载脚本
# 需 root 权限运行

if [ "$EUID" -ne 0 ]; then
  echo "请使用 root 权限运行: sudo bash uninstall.sh"
  exit 1
fi

SERVICE="use-number"
INSTALL_DIR="/opt/use-number"

echo "停止服务..."
systemctl stop $SERVICE 2>/dev/null || true
systemctl disable $SERVICE 2>/dev/null || true
rm -f /etc/systemd/system/$SERVICE.service
systemctl daemon-reload

read -p "是否同时删除程序与数据目录 $INSTALL_DIR？(y/n): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
  rm -rf "$INSTALL_DIR"
  echo "已删除 $INSTALL_DIR"
else
  echo "程序目录保留，数据未删除"
fi

echo "卸载完成"
