#!/usr/bin/env bash
# ============================================================
#  soap-web 启动脚本 (Linux / macOS)
# ============================================================
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ADDR="${1:-127.0.0.1:8420}"

# 优先使用 bin/ 下的二进制，否则用 go run
if [ -x "$SCRIPT_DIR/bin/soapweb" ]; then
  exec "$SCRIPT_DIR/bin/soapweb" -addr "$ADDR"
elif [ -x "$SCRIPT_DIR/bin/soapweb.exe" ]; then
  exec "$SCRIPT_DIR/bin/soapweb.exe" -addr "$ADDR"
else
  echo "未找到预编译二进制，使用 go run 启动..."
  cd "$SCRIPT_DIR"
  exec go run ./cmd/soapweb -addr "$ADDR"
fi
