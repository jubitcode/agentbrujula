#!/bin/bash
# Load root .env and export all vars for services that don't auto-load it
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
set -a
source "$ROOT/.env"
set +a

# The agent (research_agent/provider.py) reads OPENROUTER_MODEL; mcp-meta and
# its pods read LLM_MODEL. Two names for one decision, so one of them used to
# silently fall back to a default nobody chose. Derive each from the other
# rather than making you set both.
: "${LLM_BASE_URL:=https://openrouter.ai/api/v1}"
if [ -n "${OPENROUTER_MODEL:-}" ] && [ -z "${LLM_MODEL:-}" ]; then
  LLM_MODEL="$OPENROUTER_MODEL"
elif [ -n "${LLM_MODEL:-}" ] && [ -z "${OPENROUTER_MODEL:-}" ]; then
  OPENROUTER_MODEL="$LLM_MODEL"
fi
export LLM_BASE_URL LLM_MODEL OPENROUTER_MODEL

if [ -n "${LLM_MODEL:-}" ] && [ -n "${OPENROUTER_MODEL:-}" ] \
   && [ "$LLM_MODEL" != "$OPENROUTER_MODEL" ]; then
  echo "WARNING: LLM_MODEL=$LLM_MODEL but OPENROUTER_MODEL=$OPENROUTER_MODEL."
  echo "         The agent and mcp-meta will use different models."
fi

# This used to be built by hand here with a hardcoded port 5432, overriding
# whatever .env said. On this machine Postgres is published on 5433 (5432 is
# another project's), so mcp-meta connected to the wrong database or to
# nothing at all. .env is the single source now.
if [ -z "${DATABASE_URL:-}" ]; then
  echo "WARNING: DATABASE_URL is not set in .env; mcp-meta will use its own default."
fi

echo "=== Environment loaded ==="
echo "TOOL_REGISTRY_CONTRACT_ID=$TOOL_REGISTRY_CONTRACT_ID"
echo "X402_ESCROW_CONTRACT=$X402_ESCROW_CONTRACT"
echo "STELLAR_NETWORK=$STELLAR_NETWORK"
echo "LLM_MODEL=$LLM_MODEL"
echo ""
echo "Start each service in a separate terminal:"
echo ""
echo "  Terminal 1 (FastAPI):      ./start.sh api        # port 5050"
echo "  Terminal 2 (mcp-meta):     ./start.sh mcp        # port 9876"
echo "  Terminal 3 (Frontend):     ./start.sh web        # port 3002"
echo ""
echo "Or run all three:"
echo "  ./start.sh all"

case "${1:-}" in
  api)
    echo "Starting FastAPI on :5050..."
    cd agent && PYTHONPATH=../api uv run uvicorn server:app --reload --port 5050
    ;;
  mcp)
    echo "Building Go pods..."
    cd mcp-meta && \
      for pod in pods/adr-*/; do
        [ -f "$pod/main.go" ] && go build -o "$pod/$(basename $pod)" "./$pod" && echo "  Built $(basename $pod)"
      done
    echo "Starting mcp-meta on :9876..."
    echo "  model:    ${LLM_MODEL}"
    echo "  database: ${DATABASE_URL%%\?*}"
    \
      AUTH_ENABLED="${AUTH_ENABLED:-true}" \
      AUTH_FREE_KEY="${AUTH_FREE_KEY:?AUTH_FREE_KEY must be set}" \
      AUTH_PAID_KEY="${AUTH_PAID_KEY:?AUTH_PAID_KEY must be set}" \
      KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8180}" \
      KEYCLOAK_REALM="${KEYCLOAK_REALM:-mcp-meta}" \
      KEYCLOAK_CLIENT_ID="${KEYCLOAK_CLIENT_ID:-mcp-meta-web}" \
      go run ./cmd/mcp-meta/ --manifests ./manifests --transport api --host 0.0.0.0 --port 9876
    ;;
  web)
    echo "Starting frontend on :3002..."
    cd mcp-meta/web && npm run dev
    ;;
  all)
    echo "Starting all services..."
    ( cd "$ROOT/agent" && PYTHONPATH=../api uv run uvicorn server:app --reload --port 5050 ) &
    PID_API=$!
    # Subshells, so each service's `cd` stays its own. Before this, `cd
    # mcp-meta` persisted into the next line, which then looked for
    # mcp-meta/mcp-meta/web and never started the frontend.
    (
      cd "$ROOT/mcp-meta" && \
        AUTH_ENABLED="${AUTH_ENABLED:-true}" \
        AUTH_FREE_KEY="${AUTH_FREE_KEY:?AUTH_FREE_KEY must be set}" \
        AUTH_PAID_KEY="${AUTH_PAID_KEY:?AUTH_PAID_KEY must be set}" \
        KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8180}" \
        KEYCLOAK_REALM="${KEYCLOAK_REALM:-mcp-meta}" \
        KEYCLOAK_CLIENT_ID="${KEYCLOAK_CLIENT_ID:-mcp-meta-web}" \
        go run ./cmd/mcp-meta/ --manifests ./manifests --transport api --host 0.0.0.0 --port 9876
    ) &
    PID_MCP=$!
    ( cd "$ROOT/mcp-meta/web" && npm run dev ) &
    PID_WEB=$!
    echo ""
    echo "Services running:"
    echo "  FastAPI:   http://localhost:5050  (PID $PID_API)"
    echo "  mcp-meta:  http://localhost:9876  (PID $PID_MCP)"
    echo "  Frontend:  http://localhost:3002  (PID $PID_WEB)"
    echo ""
    echo "Press Ctrl+C to stop all"
    trap "kill $PID_API $PID_MCP $PID_WEB 2>/dev/null" EXIT INT TERM
    wait
    ;;
  *)
    # No argument — just show the help (already printed above)
    ;;
esac
