#!/usr/bin/env bash

set -eo pipefail

# Initialize variables
SECRET_NAME=""
NAMESPACE=${NAMESPACE:-kube-system}
OUTPUT_DIR=${OUTPUT_DIR:-/tmp/k8s-webhook-server/serving-certs}

# Helper function
show_help() {
  echo "Usage: $0 --secret-name SECRET_NAME --namespace NAMESPACE --output-dir OUTPUT_DIR"
  echo ""
  echo "Options:"
  echo "  -s, --secret-name NAME     Name of the Kubernetes secret"
  echo "  -n, --namespace NAMESPACE  Kubernetes namespace"
  echo "  -o, --output-dir DIR       Directory to save the output"
  echo "  -h, --help                 Show this help message"
}

# Extract options
while true; do
  case "$1" in
    -s | --secret-name ) SECRET_NAME="$2"; shift; shift ;;
    -n | --namespace ) NAMESPACE="$2"; shift; shift ;;
    -o | --output-dir ) OUTPUT_DIR="$2"; shift; shift ;;
    -h | --help ) show_help; exit 0 ;;
    -- ) shift; break ;;
    * ) break ;;
  esac
done

# Check if the required options are set
if [[ -z "$SECRET_NAME" || -z "$NAMESPACE" || -z "$OUTPUT_DIR" ]]; then
    echo "All arguments are required."
    show_help
    exit 1
fi

if ! kubectl -n "${NAMESPACE}" get secret "${SECRET_NAME}" >/dev/null 2>&1; then
  echo "can't find certs, make sure you ./kind-testx.sh once to have them generated"
  exit 1
fi

_secret_files_json=$(kubectl -n "${NAMESPACE}" get secret -ojson "${SECRET_NAME}" | jq -r '.data | to_entries')
_max_i=$(echo "$_secret_files_json" | jq '. | length - 1')

_dir=${OUTPUT_DIR}
mkdir -pv "$_dir"

for i in $(seq 0 "${_max_i}"); do
  _filename=$(echo "$_secret_files_json" | jq -r "\"${_dir}/\\(.[$i].key)\"")
  _file_contents=$(echo "$_secret_files_json" | jq -r ".[$i].value | @base64d")
  echo "$_file_contents" > "$_filename"
  echo "Wrote $_filename"
done
