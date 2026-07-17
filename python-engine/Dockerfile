FROM python:3.12-slim

WORKDIR /app

# uv バイナリを公式イメージからコピー
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# 依存解決（ソースコードより先にコピーしてレイヤーキャッシュを活かす）
COPY python-engine/pyproject.toml python-engine/uv.lock* ./
RUN uv pip compile -o requirements.txt pyproject.toml && \
    uv pip install --system -r requirements.txt

# アプリケーションコードをコピー
COPY python-engine/app/ ./app/

EXPOSE 8000

CMD ["sh", "-c", "uvicorn app.main:app --host 0.0.0.0 --port ${PORT:-8000}"]
