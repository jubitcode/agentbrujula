FROM golang:1.25.1 AS builder
WORKDIR /app

# Copy both mcp-meta and drun (go.mod has replace ../drun)
COPY mcp-meta/go.mod mcp-meta/go.sum mcp-meta/
COPY drun/go.mod drun/go.sum drun/

WORKDIR /app/mcp-meta
RUN go mod download

WORKDIR /app
COPY mcp-meta/ mcp-meta/
COPY drun/ drun/

WORKDIR /app/mcp-meta
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o mcp-meta ./cmd/mcp-meta/

# Build Go pods
RUN for pod in pods/adr-*/; do \
      if [ -f "$pod/main.go" ]; then \
        name=$(basename "$pod"); \
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "pods/$name/$name" "./$pod"; \
      fi; \
    done

FROM python:3.13-slim
WORKDIR /app

# Install uv for fast dependency resolution
COPY --from=ghcr.io/astral-sh/uv:latest /uv /usr/local/bin/uv

# Install the research_agent package (required by Python pods: hebbian, research-agent, etc.)
COPY agent/pyproject.toml agent/uv.lock* agent/
COPY agent/src/ agent/src/
RUN cd agent && uv sync --no-dev --frozen 2>/dev/null || uv sync --no-dev

# Add venv to PATH so Python pods can import research_agent
ENV PATH="/app/agent/.venv/bin:${PATH}"
ENV PYTHONPATH="/app/agent/src"

# Copy Go binary + pre-built pod binaries
COPY --from=builder /app/mcp-meta/mcp-meta .
COPY --from=builder /app/mcp-meta/pods/ pods/

# Copy manifests
COPY mcp-meta/manifests/ manifests/

EXPOSE 9876

RUN apt-get update && apt-get install -y --no-install-recommends curl && rm -rf /var/lib/apt/lists/*

ENTRYPOINT ["./mcp-meta"]
CMD ["--manifests", "./manifests", "--transport", "api", "--host", "0.0.0.0", "--port", "9876"]
