#!/bin/bash
# 一键构建 Windows / 麒麟 ARM64 双平台产物
# 依赖：Go 1.22+；纯 Go 驱动，无需 CGO
set -e

GO=${GO:-go}
OUT=dist

mkdir -p $OUT

echo "构建 Windows amd64 …"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $GO build -trimpath -ldflags "-s -w" -o $OUT/use-number-windows-amd64.exe .

echo "构建 Linux arm64（麒麟）…"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $GO build -trimpath -ldflags "-s -w" -o $OUT/use-number-kylin-arm64 .

echo "构建 Linux amd64 …"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $GO build -trimpath -ldflags "-s -w" -o $OUT/use-number-linux-amd64 .

echo "完成，产物在 $OUT/"
ls -lh $OUT/use-number-*
