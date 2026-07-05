#!/bin/bash
# コミット前ゲート: go test + golangci-lint + pytest を実行し、失敗時にコミットをブロックする
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
errors=""

# Go test
if [ -d "$ROOT/go-api" ] && command -v go >/dev/null 2>&1; then
  if ! out=$(cd "$ROOT/go-api" && go test ./... 2>&1); then
    errors="${errors}[Go test 失敗]\n${out}\n"
  fi
fi

# Go build
if [ -d "$ROOT/go-api" ] && command -v go >/dev/null 2>&1; then
  if ! out=$(cd "$ROOT/go-api" && go build ./... 2>&1); then
    errors="${errors}[Go build 失敗]\n${out}\n"
  fi
fi

# Go lint
if [ -d "$ROOT/go-api" ] && command -v golangci-lint >/dev/null 2>&1; then
  if ! out=$(cd "$ROOT/go-api" && golangci-lint run ./... 2>&1); then
    errors="${errors}[golangci-lint 失敗]\n${out}\n"
  fi
fi

# Python test
if [ -d "$ROOT/python-engine" ] && command -v pytest >/dev/null 2>&1; then
  if ! out=$(cd "$ROOT/python-engine" && python -m pytest 2>&1); then
    errors="${errors}[pytest 失敗]\n${out}\n"
  fi
fi

if [ -n "$errors" ]; then
  printf '{"continue": false, "stopReason": %s}\n' "$(printf '%s' "$errors" | jq -Rs .)"
fi
