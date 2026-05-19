#!/bin/bash
# Load root .env and export all vars for services that don't auto-load it
set -a
source .env
set +a

echo "=== Environment loaded ==="
echo "TOOL_REGISTRY_CONTRACT_ID=$TOOL_REGISTRY_CONTRACT_ID"
echo "X402_ESCROW_CONTRACT=$X402_ESCROW_CONTRACT"
echo "STELLAR_NETWORK=$STELLAR_NETWORK"
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
    \
      LLM_BASE_URL="${LLM_BASE_URL:-http://localhost:1234/v1}" \
      AUTH_ENABLED="${AUTH_ENABLED:-false}" \
      AUTH_FREE_KEY="${AUTH_FREE_KEY:-mcp-dev-key}" \
      AUTH_PAID_KEY="${AUTH_PAID_KEY:-mcp-dev-key}" \
      KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8180}" \
      KEYCLOAK_REALM="${KEYCLOAK_REALM:-mcp-meta}" \
      KEYCLOAK_CLIENT_ID="${KEYCLOAK_CLIENT_ID:-mcp-meta-web}" \
      LLM_BASE_URL=https://openrouter.ai/api/v1  \
      LLM_MODEL=deepseek/deepseek-chat \
      DATABASE_URL="postgresql://agent_web3:dev-password@localhost:5432/marketplace" \
      go run ./cmd/mcp-meta/ --manifests ./manifests --transport api --host 0.0.0.0 --port 9876
      # ./mcp-meta --manifests ./manifests --transport api --host 0.0.0.0 --port 9876
    ;;
  web)
    echo "Starting frontend on :3002..."
    cd mcp-meta/web && npm run dev
    ;;
  all)
    echo "Starting all services..."
    cd agent && PYTHONPATH=../api uv run uvicorn server:app --reload --port 5050 &
    PID_API=$!
    cd mcp-meta && \
      LLM_BASE_URL="${LLM_BASE_URL:-http://localhost:1234/v1}" \
      AUTH_ENABLED="${AUTH_ENABLED:-true}" \
      AUTH_FREE_KEY="${AUTH_FREE_KEY:-mcp-dev-key}" \
      AUTH_PAID_KEY="${AUTH_PAID_KEY:-mcp-dev-key}" \
      KEYCLOAK_URL="${KEYCLOAK_URL:-http://localhost:8180}" \
      KEYCLOAK_REALM="${KEYCLOAK_REALM:-mcp-meta}" \
      KEYCLOAK_CLIENT_ID="${KEYCLOAK_CLIENT_ID:-mcp-meta-web}" \
      ./mcp-meta --manifests ./manifests --transport api --host 0.0.0.0 --port 9876 &
    PID_MCP=$!
    cd mcp-meta/web && npm run dev &
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
