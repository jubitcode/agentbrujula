FROM python:3.13-slim AS builder

COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

WORKDIR /app

COPY agent/pyproject.toml agent/uv.lock* agent/
COPY agent/src/ agent/src/

RUN cd agent && uv sync --no-dev --frozen 2>/dev/null || uv sync --no-dev

COPY api/server.py api/stellar_health.py api/

COPY .env.example .env.example

FROM python:3.13-slim

WORKDIR /app

COPY --from=builder /app/agent/.venv /app/agent/.venv
COPY --from=builder /app/agent/src /app/agent/src
COPY --from=builder /app/api /app/api
COPY --from=builder /app/.env.example /app/.env.example

ENV PATH="/app/agent/.venv/bin:${PATH}"
ENV PYTHONPATH="/app/api:/app/agent/src"

EXPOSE 5050

CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "5050"]
