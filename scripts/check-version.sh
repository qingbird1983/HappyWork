#!/usr/bin/env bash
# check-version —— 发布门禁：tag 去掉 v 前缀必须等于 build/config.yml 的 info.version。
# 用法：bash scripts/check-version.sh v0.1.0（CI 中缺省读 GITHUB_REF_NAME）。
# 不一致即失败，绝不自动改写配置（ADR-0017：发布物与版本配置必须同源）。
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

REF="${1:-${GITHUB_REF_NAME:-}}"

if [[ ! "$REF" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)$ ]]; then
  echo "错误：发布标签必须是 vX.Y.Z 形式，收到 '${REF:-<空>}'" >&2
  exit 2
fi
TAG_VERSION="${BASH_REMATCH[1]}"

if [[ ! -f build/config.yml ]]; then
  echo "错误：缺少 build/config.yml" >&2
  exit 2
fi

# 取 info: 段内的 version（跳过文件顶层的 `version: '3'` schema 键）。
CONFIG_VERSION="$(
  sed -n '/^info:/,/^[^[:space:]#]/p' build/config.yml |
    sed -n 's/^[[:space:]]*version:[[:space:]]*"\{0,1\}\([^"#[:space:]]*\)"\{0,1\}.*/\1/p' |
    head -n 1
)"

if [[ -z "$CONFIG_VERSION" ]]; then
  echo "错误：build/config.yml 的 info 段没有 version 字段" >&2
  exit 2
fi

if [[ "$TAG_VERSION" != "$CONFIG_VERSION" ]]; then
  echo "错误：标签版本 $TAG_VERSION 与 build/config.yml 的 $CONFIG_VERSION 不一致" >&2
  echo "     请先提交版本变更（build/config.yml + CHANGELOG.md）再打标签。" >&2
  exit 1
fi

echo "OK：版本一致（$TAG_VERSION）"

# 生成物版本也要跟上：exe 属性、NSIS 定义、Linux 包元数据、Info.plist。
# 否则发布物自报版本与标签不一致，用户即使验签通过也会看到旧版本号。
fail=0
check_asset() {
  local file="$1" pattern="$2"
  [[ -f "$file" ]] || return 0
  if ! grep -q "$pattern" "$file"; then
    echo "错误：$file 未包含版本 $TAG_VERSION" >&2
    fail=1
  fi
}
check_asset build/windows/info.json "\"file_version\": \"$TAG_VERSION\""
check_asset build/windows/info.json "\"ProductVersion\": \"$TAG_VERSION\""
check_asset build/windows/nsis/wails_tools.nsh "!define INFO_PRODUCTVERSION \"$TAG_VERSION\""
check_asset build/linux/nfpm/nfpm.yaml "^version: \"$TAG_VERSION\""
if [[ -f build/darwin/Info.plist ]]; then
  if ! grep -A1 CFBundleShortVersionString build/darwin/Info.plist |
    grep -q "<string>$TAG_VERSION</string>"; then
    echo "错误：build/darwin/Info.plist 未包含版本 $TAG_VERSION" >&2
    fail=1
  fi
fi
if [[ "$fail" == "1" ]]; then
  echo "     运行 wails3 task common:update:build-assets 并提交生成物后重试。" >&2
  exit 1
fi
echo "OK：构建生成物版本一致（$TAG_VERSION）"
