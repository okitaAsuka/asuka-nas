#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# 使用显式真实工具链，避免环境中的同名非 Go 程序。
: "${GO_BINARY:?Set GO_BINARY to the absolute Go 1.26.9 executable}"
case "$("$GO_BINARY" version)" in
  'go version go1.26.9 '*) ;;
  *) printf 'Go 1.26.9 required\n' >&2; exit 1 ;;
esac
export PATH="$(dirname "$GO_BINARY"):$PATH"
exec make verify
