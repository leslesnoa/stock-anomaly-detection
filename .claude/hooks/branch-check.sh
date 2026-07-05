#!/bin/bash
# mainブランチでコードファイルを直接編集するのをブロック

file=$(jq -r '.tool_input.file_path // empty' 2>/dev/null)
if [ -z "$file" ]; then exit 0; fi

# コードファイルのみチェック（設定ファイル・ドキュメントは除外）
if ! echo "$file" | grep -qE '\.(go|py|ts|tsx|js|jsx|sql)$'; then
  exit 0
fi

# プロジェクトルートからブランチ名を取得
ROOT=$(git rev-parse --show-toplevel 2>/dev/null)
if [ -z "$ROOT" ]; then exit 0; fi

branch=$(git -C "$ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null)

if [ "$branch" = "main" ] || [ "$branch" = "master" ]; then
  printf '{"continue": false, "stopReason": "mainブランチで直接コードを編集しようとしています。\\nfeatureブランチを作成してから作業を開始してください:\\n  git checkout -b feature/<task-name>\\n\\nブランチ作成後に再度お試しください。"}\n'
fi
