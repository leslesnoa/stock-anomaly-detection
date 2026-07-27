FROM python:3.12-slim

WORKDIR /app

# uv バイナリを公式イメージからコピー
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# システムPythonを使用するよう設定
ENV UV_SYSTEM_PYTHON=1

# 依存解決（ソースコードより先にコピーしてレイヤーキャッシュを活かす）
# uv.lock を使用した再現可能なビルド
COPY python-engine/pyproject.toml python-engine/uv.lock ./
RUN uv sync --no-dev --frozen

# アプリケーションコードをコピー
COPY python-engine/app/ ./app/

EXPOSE 8000

CMD ["sh", "-c", "uvicorn app.main:app --host 0.0.0.0 --port ${PORT:-8000}"]
