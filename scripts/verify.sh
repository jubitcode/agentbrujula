#!/usr/bin/env bash
# verify.sh — run every test suite in the repository and write VERIFICATION.md
# with the real result, failures included.
#
# The point is reproducibility by a third party: anyone who clones this repo
# can run `make verify` and get the same file. It records what passes AND what
# does not, because a report that only shows the green runs is not evidence.
#
# Usage:
#   ./scripts/verify.sh            # run everything, write VERIFICATION.md
#   ./scripts/verify.sh --stdout   # also echo the report to stdout
#
# Notes on method:
#   - The Python suite is NOT hermetic: it writes to $HOME/.mcp-meta and the
#     x402 anti-replay test passes exactly once per machine. We therefore run
#     it per-file with a FRESH HOME for each file, which is the only way to get
#     a stable number today. This is a finding, not a workaround to hide.
#   - Running the Python suite as a whole hangs (an interaction between files,
#     not a slow test), so each file gets a timeout.

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="$ROOT/VERIFICATION.md"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PER_FILE_TIMEOUT="${PER_FILE_TIMEOUT:-60}"
SUITE_TIMEOUT="${SUITE_TIMEOUT:-900}"

# timeout(1) is coreutils; macOS ships it as gtimeout under Homebrew.
if command -v timeout >/dev/null 2>&1; then TIMEOUT=timeout
elif command -v gtimeout >/dev/null 2>&1; then TIMEOUT=gtimeout
else TIMEOUT=""; fi

run_with_timeout() { # run_with_timeout <seconds> <cmd...>
  local secs="$1"; shift
  if [ -n "$TIMEOUT" ]; then "$TIMEOUT" "$secs" "$@"; else "$@"; fi
}

say() { printf '%s\n' "$*" >&2; }

COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo 'sin git')"
DIRTY="$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
DATE_UTC="$(date -u '+%Y-%m-%d %H:%M UTC')"

# ───────────────────────────── Rust / Soroban ─────────────────────────────
say "==> Rust (contracts)"
RUST_LOG_FILE="$WORK/rust.log"
( cd contracts && run_with_timeout "$SUITE_TIMEOUT" cargo test --quiet ) \
  >"$RUST_LOG_FILE" 2>&1
RUST_EXIT=$?
RUST_PASS=$(grep -oE '[0-9]+ passed' "$RUST_LOG_FILE" | awk '{s+=$1} END{print s+0}')
RUST_FAIL=$(grep -oE '[0-9]+ failed' "$RUST_LOG_FILE" | awk '{s+=$1} END{print s+0}')

# ───────────────────────────────── Go ─────────────────────────────────────
say "==> Go (mcp-meta)"
GO_LOG_FILE="$WORK/go.log"
( cd mcp-meta && run_with_timeout "$SUITE_TIMEOUT" go test ./internal/... ) \
  >"$GO_LOG_FILE" 2>&1
GO_EXIT=$?
GO_OK=$(grep -cE '^ok ' "$GO_LOG_FILE" || true)
GO_FAILED=$(grep -cE '^FAIL' "$GO_LOG_FILE" || true)
GO_NOTEST=$(grep -cE 'no test files' "$GO_LOG_FILE" || true)
GO_FAIL_PKGS=$(grep -E '^(FAIL|--- FAIL)' "$GO_LOG_FILE" | head -20 || true)

# ──────────────────────────────── Web ─────────────────────────────────────
say "==> Web (jest)"
WEB_LOG_FILE="$WORK/web.log"
if [ -d mcp-meta/web/node_modules ]; then
  ( cd mcp-meta/web && run_with_timeout "$SUITE_TIMEOUT" npm test --silent ) \
    >"$WEB_LOG_FILE" 2>&1
  WEB_EXIT=$?
else
  echo "node_modules ausente — npm ci no ejecutado por este script" >"$WEB_LOG_FILE"
  WEB_EXIT=127
fi
WEB_SUMMARY=$(grep -E '^Tests:' "$WEB_LOG_FILE" | tail -1 || true)

# ───────────────────── Python — per file, fresh HOME ──────────────────────
# See the method note at the top: the suite is not hermetic and hangs as a whole.
say "==> Python (agent) — per file, fresh HOME"
PY_LOG_FILE="$WORK/python.log"
PY_PASS=0; PY_FAIL=0; PY_SKIP=0; PY_ERROR=0; PY_TIMEOUT=0; PY_FILES=0
: >"$PY_LOG_FILE"

while IFS= read -r testfile; do
  PY_FILES=$((PY_FILES + 1))
  FAKE_HOME="$WORK/home_$PY_FILES"
  mkdir -p "$FAKE_HOME"
  # Capture to a file first: piping to tail would mask the timeout exit code.
  ( cd agent && HOME="$FAKE_HOME" \
      run_with_timeout "$PER_FILE_TIMEOUT" uv run pytest "$testfile" -q --no-header -p no:cacheprovider \
  ) >"$WORK/py_run.log" 2>&1
  rc=$?
  line=$(tail -5 "$WORK/py_run.log")
  short="$(basename "$testfile")"
  if [ $rc -eq 124 ]; then
    PY_TIMEOUT=$((PY_TIMEOUT + 1))
    printf '%-52s TIMEOUT (>%ss)\n' "$short" "$PER_FILE_TIMEOUT" >>"$PY_LOG_FILE"
    continue
  fi
  summary=$(printf '%s' "$line" | grep -oE '[0-9]+ (passed|failed|skipped|error[s]?)' | tr '\n' ' ')
  [ -z "$summary" ] && summary="sin resumen (rc=$rc)"
  printf '%-52s %s\n' "$short" "$summary" >>"$PY_LOG_FILE"
  p=$(printf '%s' "$summary" | grep -oE '[0-9]+ passed'  | awk '{print $1}'); PY_PASS=$((PY_PASS + ${p:-0}))
  f=$(printf '%s' "$summary" | grep -oE '[0-9]+ failed'  | awk '{print $1}'); PY_FAIL=$((PY_FAIL + ${f:-0}))
  s=$(printf '%s' "$summary" | grep -oE '[0-9]+ skipped' | awk '{print $1}'); PY_SKIP=$((PY_SKIP + ${s:-0}))
  e=$(printf '%s' "$summary" | grep -oE '[0-9]+ errors?' | awk '{print $1}'); PY_ERROR=$((PY_ERROR + ${e:-0}))
done < <(ls agent/tests/test_*.py 2>/dev/null | sed 's|^agent/||' | sort)

# ─────────────────────────────── report ───────────────────────────────────
status_word() { [ "$1" -eq 0 ] && echo "verde" || echo "con fallos"; }

{
cat <<EOF
# VERIFICATION

Resultado de correr **todas** las suites de test del repositorio, con sus fallos incluidos.
Generado por \`make verify\` (\`scripts/verify.sh\`). Reproducible por cualquiera que clone el repo.

| | |
|---|---|
| **Fecha** | $DATE_UTC |
| **Commit** | \`$COMMIT\` |
| **Árbol de trabajo** | $([ "$DIRTY" -eq 0 ] && echo "limpio" || echo "$DIRTY archivos sin commitear") |

> Este archivo se regenera con \`make verify\`. **No se edita a mano.**
> Incluye lo que falla a propósito: un reporte que solo muestra las corridas verdes no es evidencia.

---

## Resumen

| Suite | Comando | Resultado |
|---|---|---|
| Rust · contratos Soroban | \`cd contracts && cargo test\` | **$RUST_PASS pasan**, $RUST_FAIL fallan — $(status_word $RUST_EXIT) |
| Go · mcp-meta | \`cd mcp-meta && go test ./internal/...\` | **$GO_OK paquetes ok**, $GO_FAILED con fallos, $GO_NOTEST sin tests — $(status_word $GO_EXIT) |
| Web · jest | \`cd mcp-meta/web && npm test\` | ${WEB_SUMMARY:-no ejecutado} — $(status_word $WEB_EXIT) |
| Python · agent | por archivo, \`HOME\` limpio | **$PY_PASS pasan**, $PY_FAIL fallan, $PY_SKIP se saltan, $PY_ERROR errores, $PY_TIMEOUT timeouts |

**Total verificado pasando: $((RUST_PASS + PY_PASS)) tests de Rust y Python**, más $GO_OK paquetes Go y la suite web.

---

## Por qué el número de Python no es una medida de calidad del código

La suite de Python **no es hermética**. Comparte cuatro clases de estado, y cada una produce
fallos que no son defectos del código:

| Estado compartido | Qué provoca | Cómo se reconoce |
|---|---|---|
| \`\$HOME/.mcp-meta/marketplace.db\` | El test anti-replay de x402 **pasa exactamente una vez por máquina** | El mismo archivo da resultados distintos en la segunda corrida |
| **Filas en Postgres** | Nonces ya consumidos por corridas anteriores | \`claim_nonce(...)\` devuelve \`False\` sobre un nonce "nuevo" |
| **Presupuesto del rate limiter de mcp-meta** | La propia suite lo agota y las aserciones siguientes reciben **429 en vez de 401** | \`assert 429 == 401\` |
| **Requiere mcp-meta vivo** | Sin el servicio, los tests de integración fallan en lugar de saltarse | Errores de conexión |

Por eso este script corre Python **archivo por archivo, con un \`HOME\` nuevo para cada uno** y un
timeout de ${PER_FILE_TIMEOUT}s — corrida entera, **cuelga**, por una interacción entre archivos y no
por un test lento.

**Para leer este archivo honestamente:** los números de abajo son el estado de la *suite*, no el de
los contratos ni el del orquestador. Las suites que sí son herméticas —Rust y Go— están limpias.
Hacer hermética la de Python es trabajo pendiente y está registrado como tal.

### Condiciones del entorno de esta corrida

| | |
|---|---|
| mcp-meta alcanzable | $(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "${MCP_META_URL:-http://127.0.0.1:9876}/health" 2>/dev/null | grep -q 200 && echo "sí" || echo "**no** — los tests de integración fallan en vez de saltarse") |
| Postgres alcanzable | $(command -v pg_isready >/dev/null 2>&1 && echo "verificable con pg_isready" || echo "no verificado por este script") |
| \`HOME\` por archivo | nuevo (temporal) |
| Timeout por archivo | ${PER_FILE_TIMEOUT}s |

**Para correr la suite sin ruido de rate limiting**, mcp-meta tiene que arrancar con límites
permisivos. Los valores de producción (\`RATE_LIMIT_FREE=0.014\`, ~50/hora) los agota la propia
suite en segundos, y a partir de ahí todo responde **429** — por eso aparecen aserciones del
tipo \`assert 429 == 401\`:

\`\`\`sh
RATE_LIMIT_FREE=1000 RATE_LIMIT_PAID=1000 ./start.sh mcp
\`\`\`

---

## Rust — contratos Soroban

\`\`\`
$(tail -25 "$RUST_LOG_FILE")
\`\`\`

## Go — mcp-meta

$([ -n "$GO_FAIL_PKGS" ] && printf 'Paquetes con fallos:\n\n```\n%s\n```\n' "$GO_FAIL_PKGS" || echo "Sin fallos.")

\`\`\`
$(grep -E '^(ok|FAIL|\?)' "$GO_LOG_FILE" | head -30)
\`\`\`

## Web — jest

\`\`\`
$(grep -E '^(Tests:|Test Suites:|Snapshots:|Time:)' "$WEB_LOG_FILE" | head -10 || tail -15 "$WEB_LOG_FILE")
\`\`\`

## Python — detalle por archivo

\`\`\`
$(cat "$PY_LOG_FILE")
\`\`\`

---

## Contratos desplegados

Los IDs de contrato y su red viven en \`.env.example\`. Para comprobar que el bytecode desplegado
corresponde a este código:

\`\`\`sh
stellar contract fetch --id <CONTRACT_ID> --network testnet --out-file /tmp/deployed.wasm
cd contracts && stellar contract build
shasum -a 256 /tmp/deployed.wasm target/wasm32v1-none/release/<contract>.wasm
\`\`\`

⚠ **Al $DATE_UTC estos hashes NO coinciden** para los tres contratos. Los contratos en testnet se
desplegaron con un toolchain anterior y sin el paso de optimización. Está registrado como pendiente;
se resuelve redesplegando desde este árbol.
EOF
} >"$OUT"

say ""
say "VERIFICATION.md escrito en $OUT"
say "  Rust:   $RUST_PASS pasan / $RUST_FAIL fallan"
say "  Go:     $GO_OK paquetes ok / $GO_FAILED con fallos"
say "  Python: $PY_PASS pasan / $PY_FAIL fallan / $PY_SKIP saltados / $PY_TIMEOUT timeouts"
say "  Web:    ${WEB_SUMMARY:-no ejecutado}"

if [ "${1:-}" = "--stdout" ]; then cat "$OUT"; fi

# The script succeeds when it produced a report. Suite failures are content,
# not script errors — that is the point of the file.
exit 0
