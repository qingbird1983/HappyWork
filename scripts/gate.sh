#!/usr/bin/env bash
# gate —— 本地与远端同源的门禁脚本（所有者 2026-10-31 决定，ADR-0018）。
#
# 为什么要有这个脚本：开发机就是 Windows，日常验收不该每次都占用 Actions 分钟
# （Linux 1× / Windows 2× / macOS 10× 计费的配额资源）。本地跑的就是 CI 里那一套命令，
# 两边不再各写一份、各自漂移。
#
# 用法：
#   bash scripts/gate.sh              # 全部门禁（不含平台构建）
#   bash scripts/gate.sh --race       # 追加 go test -race
#   bash scripts/gate.sh --build      # 追加 wails3 build（本机对应平台）
#   bash scripts/gate.sh --skip-lint  # 已单独跑过 golangci-lint 时使用（CI 里就是这样）
#
# 说明：格式化检查会把工作树拷到临时目录并去掉 CR 再比对——本机 core.autocrlf=true
# 时 `gofmt -l .` 会把每个 CRLF 文件都误报为未格式化（P-013 同一类"平台差异"）。
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

RACE=0
BUILD=0
SKIP_LINT=0
for arg in "$@"; do
  case "$arg" in
    --race) RACE=1 ;;
    --build) BUILD=1 ;;
    --skip-lint) SKIP_LINT=1 ;;
    *) echo "gate: 未知参数 $arg" >&2; exit 2 ;;
  esac
done

step() { printf '\n──────── %s ────────\n' "$1"; }

step "Go 格式化（去 CR 后按 CI 语义比对）"
tmpd="$(mktemp -d)"
trap 'rm -rf "$tmpd"' EXIT
while IFS= read -r f; do
  mkdir -p "$tmpd/$(dirname "$f")"
  tr -d '\r' <"$f" >"$tmpd/$f"
done < <(git ls-files '*.go')
unformatted="$(gofmt -l "$tmpd" || true)"
if [[ -n "$unformatted" ]]; then
  echo "错误：以下文件未格式化（列出的是临时副本路径）" >&2
  echo "$unformatted" >&2
  exit 1
fi
echo "OK：gofmt 无差异"

step "go build"
go build ./...

step "go vet"
go vet ./...

if [[ "$SKIP_LINT" == "1" ]]; then
  step "golangci-lint：已由外部执行，跳过"
elif ! command -v golangci-lint >/dev/null 2>&1; then
  echo "错误：未安装 golangci-lint（硬门槛，standards/01）。安装后重试，或用 --skip-lint 表明已在别处跑过。" >&2
  exit 1
else
  step "golangci-lint"
  golangci-lint run ./...
fi

step "go test"
if [[ "$RACE" == "1" ]]; then
  go test -race ./...
else
  go test ./...
fi

step "前端门禁（format/lint/typecheck/unit）"
if [[ -d frontend ]]; then
  if [[ ! -d frontend/node_modules ]]; then
    (cd frontend && npm ci)
  fi
  (cd frontend && npm run format:check && npm run lint && npm run typecheck && npm run test:unit)
fi

step "渲染安全与体量哨兵"
bash scripts/check-render-safety.sh
bash scripts/check-size

if [[ "$BUILD" == "1" ]]; then
  step "wails3 build（当前平台）"
  if ! command -v wails3 >/dev/null 2>&1; then
    echo "错误：未找到 wails3 CLI" >&2
    exit 1
  fi
  wails3 build
fi

printf '\n════════ gate: 全部通过 ════\n'
