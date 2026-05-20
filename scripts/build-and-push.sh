#!/usr/bin/env bash
# build-and-push.sh — build container images in ACR and record their tags.
#
# Usage:
#   scripts/build-and-push.sh [api|mcp-meta|all]    # default: all
#
# Writes scripts/.image-tags with `api=<sha>` / `mcp-meta=<sha>` so that
# the terraform/apps apply can read them:
#   terraform apply \
#     -var api_image_tag=$(awk -F= '/^api=/{print $2}' scripts/.image-tags) \
#     -var mcp_meta_image_tag=$(awk -F= '/^mcp-meta=/{print $2}' scripts/.image-tags)

set -euo pipefail

TARGET="${1:-all}"

case "${TARGET}" in
  api|mcp-meta|all) ;;
  *)
    echo "usage: $0 [api|mcp-meta|all]" >&2
    exit 2
    ;;
esac

if ! command -v az >/dev/null; then
  echo "az CLI not installed." >&2
  exit 1
fi

if ! command -v git >/dev/null; then
  echo "git not installed." >&2
  exit 1
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

SHA="$(git rev-parse --short=12 HEAD)"
if [[ -n "$(git status --porcelain)" ]]; then
  SHA="${SHA}-dirty"
  echo "Warning: working tree is dirty; tagging as ${SHA}" >&2
fi

ACR_NAME="$(
  az acr list \
    --query "[?starts_with(name, 'acragentcanonical') || tags.project=='agent-canonical'].name | [0]" \
    -o tsv
)"

if [[ -z "${ACR_NAME}" || "${ACR_NAME}" == "None" ]]; then
  echo "No ACR found (looked for name prefix acragentcanonical or tag project=agent-canonical)." >&2
  echo "Has terraform/platform been applied?" >&2
  exit 1
fi

echo "ACR:    ${ACR_NAME}"
echo "Tag:    ${SHA}"
echo

mkdir -p scripts
TAGFILE="scripts/.image-tags"
touch "${TAGFILE}"

build_one() {
  local name="$1"
  local dockerfile="$2"

  echo "==> Building ${name} from ${dockerfile}"
  az acr build \
    --registry "${ACR_NAME}" \
    --image "${name}:${SHA}" \
    --file "${dockerfile}" \
    .

  # Update the tagfile with this image's SHA
  if grep -q "^${name}=" "${TAGFILE}"; then
    sed -i.bak "s|^${name}=.*|${name}=${SHA}|" "${TAGFILE}"
    rm -f "${TAGFILE}.bak"
  else
    echo "${name}=${SHA}" >> "${TAGFILE}"
  fi

  echo "==> ${name}:${SHA} pushed"
  echo
}

if [[ "${TARGET}" == "api" || "${TARGET}" == "all" ]]; then
  build_one "api" "api/Dockerfile"
fi

if [[ "${TARGET}" == "mcp-meta" || "${TARGET}" == "all" ]]; then
  build_one "mcp-meta" "mcp-meta/Dockerfile"
fi

echo "Tags recorded in ${TAGFILE}:"
cat "${TAGFILE}"
