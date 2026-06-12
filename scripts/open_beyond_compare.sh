#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="$ROOT_DIR/.local"
BINARY="$BIN_DIR/tcd-beyond-compare"
ADDR="${TCD_BEYOND_COMPARE_ADDR:-127.0.0.1:18767}"
HEALTH_URL="http://$ADDR/api/health"
BASE_URL="http://$ADDR/"
NO_OPEN=0

usage() {
  cat <<'EOF'
用法：
  open_beyond_compare.sh <left_path> <right_path> [--no-open]

说明：
  - 自动构建并启动 tcd-beyond-compare 本地审阅台
  - 自动把左右路径带进页面
  - 默认自动在浏览器打开
EOF
}

if [[ $# -lt 2 ]]; then
  usage
  exit 1
fi

LEFT_PATH=""
RIGHT_PATH=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-open)
      NO_OPEN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      if [[ -z "$LEFT_PATH" ]]; then
        LEFT_PATH="$1"
      elif [[ -z "$RIGHT_PATH" ]]; then
        RIGHT_PATH="$1"
      else
        echo "多余参数：$1" >&2
        exit 1
      fi
      shift
      ;;
  esac
done

if [[ ! -e "$LEFT_PATH" ]]; then
  echo "左路径不存在：$LEFT_PATH" >&2
  exit 1
fi

if [[ ! -e "$RIGHT_PATH" ]]; then
  echo "右路径不存在：$RIGHT_PATH" >&2
  exit 1
fi

mkdir -p "$BIN_DIR" "$ROOT_DIR/logs"

build_if_needed() {
  local rebuild=0
  if [[ ! -x "$BINARY" ]]; then
    rebuild=1
  elif [[ "$ROOT_DIR/cmd/tcd-beyond-compare/main.go" -nt "$BINARY" || "$ROOT_DIR/cmd/tcd-beyond-compare/index.html" -nt "$BINARY" || "$ROOT_DIR/go.mod" -nt "$BINARY" ]]; then
    rebuild=1
  fi

  if [[ "$rebuild" -eq 1 ]]; then
    (cd "$ROOT_DIR" && go build -o "$BINARY" ./cmd/tcd-beyond-compare)
  fi
}

ensure_running() {
  if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
    return 0
  fi

  local log_file="$ROOT_DIR/logs/tcd_beyond_compare_$(date +%Y%m%d_%H%M%S).log"
  (
    cd "$ROOT_DIR"
    nohup "$BINARY" >"$log_file" 2>&1 &
  )

  for _ in $(seq 1 20); do
    if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.3
  done

  echo "本地审阅台启动失败，请查看日志：$log_file" >&2
  exit 1
}

urlencode() {
  local s="$1"
  local out=""
  local i ch hex
  for ((i = 0; i < ${#s}; i++)); do
    ch="${s:i:1}"
    case "$ch" in
      [a-zA-Z0-9.~_-])
        out+="$ch"
        ;;
      *)
        printf -v hex '%%%02X' "'$ch"
        out+="$hex"
        ;;
    esac
  done
  printf '%s' "$out"
}

build_if_needed
ensure_running

LEFT_ENC="$(urlencode "$(cd "$(dirname "$LEFT_PATH")" && pwd)/$(basename "$LEFT_PATH")")"
RIGHT_ENC="$(urlencode "$(cd "$(dirname "$RIGHT_PATH")" && pwd)/$(basename "$RIGHT_PATH")")"
URL="${BASE_URL}?left=${LEFT_ENC}&right=${RIGHT_ENC}"

if [[ "$NO_OPEN" -eq 0 ]]; then
  open "$URL"
fi

cat <<EOF
已就绪：
left=$LEFT_PATH
right=$RIGHT_PATH
url=$URL
EOF
