#!/bin/bash
# Stop hook: Claudeの作業完了時にlint結果をフィードバックする
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
msg=""

# Go lint
if [ -d "$ROOT/go-api" ] && command -v golangci-lint >/dev/null 2>&1; then
  if out=$(cd "$ROOT/go-api" && golangci-lint run ./... 2>&1); then
    msg="${msg}[Go lint] クリア\n"
  else
    msg="${msg}[Go lint 警告]\n${out}\n"
  fi
fi

# Python lint (ruff)
if [ -d "$ROOT/python-engine" ] && command -v uv >/dev/null 2>&1; then
  if out=$(cd "$ROOT/python-engine" && uv run ruff check . 2>&1); then
    msg="${msg}[Python lint] クリア\n"
  else
    msg="${msg}[Python lint 警告]\n${out}\n"
  fi
fi

if [ -n "$msg" ]; then
  printf '{"systemMessage": %s}\n' "$(printf '%s' "$msg" | jq -Rs .)"
fi
