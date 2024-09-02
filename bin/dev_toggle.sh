#!/usr/bin/env bash

set -eo pipefail

SCRIPT_DIR="$(dirname -- "${BASH_SOURCE[0]}")"
DEFAULT_KUSTOMIZE="$(realpath "${SCRIPT_DIR}/kustomize")"
KUSTOMIZE="${KUSTOMIZE:-$DEFAULT_KUSTOMIZE}"

if [[ ! -x "$KUSTOMIZE" ]]; then
  echo "ERROR: Make sure kustomize is available at $KUSTOMIZE by running 'make kustomize' or set the KUSTOMIZE environment variable appropriately" >&2
fi

# Figure out if we're in dev mode or in normal mode
_is_dev_mode=$(grep service-dev.yaml "${SCRIPT_DIR}/../deploy/kustomization.yaml" >/dev/null 2>&1 || echo nope)
if [[ "$_is_dev_mode" == "nope" ]]; then
  echo "Switching to 'development' mode (webhooks runs on workstation)"

  "${SCRIPT_DIR}/extract_k8s_secret.sh" -n kube-system -s node-specific-sizing-cert

  # Figure out the bridge address (that's going to be fun to port to MacOS ...)
  _gateway_ip=$(docker inspect k3d-knss-server-0 | jq '.[0].NetworkSettings.Networks | to_entries | .[0].value.Gateway')

  cd "${SCRIPT_DIR}/../deploy"
  sed -ri "s/- ip: \"[^\"]+\"/- ip: ${_gateway_ip}/" "service-dev.yaml"
  "$KUSTOMIZE" edit add resource service-dev.yaml
  "$KUSTOMIZE" edit remove resource service.yaml

  cd .. && make deploy
else
  echo "Switching to 'regular service' mode (webhooks runs inside kubernetes)"

  cd "${SCRIPT_DIR}/../deploy"
  "$KUSTOMIZE" edit remove resource service-dev.yaml
  "$KUSTOMIZE" edit add resource service.yaml

  kubectl -n kube-system
  cd .. && make deploy
fi




