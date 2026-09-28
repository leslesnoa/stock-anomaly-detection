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

# uv sync は UV_SYSTEM_PYTHON の設定に関わらず /app/.venv に仮想環境を作成するため、
# その bin を PATH に通してから uvicorn を起動する（これが無いと "uvicorn: not found" で
# コンテナが即座にクラッシュする）
ENV PATH="/app/.venv/bin:${PATH}"

EXPOSE 8000

CMD ["sh", "-c", "uvicorn app.main:app --host :: --port ${PORT:-8000}"]
