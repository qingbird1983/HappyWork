#!/usr/bin/env bash
# A6 static gate: forbid HTML-injection APIs in the renderer and require the
# Phase 2 CSP directives in index.html.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
if grep -RInE 'dangerouslySetInnerHTML|innerHTML|outerHTML|document\.write' frontend/src; then
  echo "check-render-safety: forbidden HTML-injection API found" >&2
  fail=1
fi

csp_line=$(grep -A3 -i 'Content-Security-Policy' frontend/index.html | grep -o 'content="[^"]*"' | head -1)
for directive in default-src script-src style-src img-src font-src connect-src frame-src object-src base-uri form-action; do
  if ! printf '%s' "$csp_line" | grep -q "$directive"; then
    echo "check-render-safety: CSP missing $directive" >&2
    fail=1
  fi
done
if ! printf '%s' "$csp_line" | grep -q "frame-src 'none'"; then
  echo "check-render-safety: frame-src must be 'none'" >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "check-render-safety: ok"
