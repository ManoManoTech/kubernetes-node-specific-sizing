#!/usr/bin/bash

set -eo pipefail

SCRIPT_DIR="$(dirname -- "${BASH_SOURCE[0]}")"
DEFAULT_KUSTOMIZE="$(realpath "${SCRIPT_DIR}/../bin/kustomize")"
KUSTOMIZE="${KUSTOMIZE:-$DEFAULT_KUSTOMIZE}"
K3D_CLUSTER="${K3D_CLUSTER:-knss}"

RECREATE_CLUSTER=0
WITH_BUILD=0
WITH_DEPLOY=0
WITH_WORKLOAD=0
OPT_IN_MODE=0

show_help() {
  cat << EOF
  Usage: $0 --recreate-cluster | --with-build | --with-deploy | --with-workload

  Will do everything by default except recreating the playground K3D cluster.
  Setting any of the --with-x options will reverse that behavior, which will
  only run the selected parts.

  Options:
  -r, --recreate-cluster     Drop and recreate the playground K3D cluster
  -b, --with-build           Build the current version of the webhook docker image
                             and make it available in the playground cluster
  -d, --with-deploy          Re-renders the various manifests required to setup the
                             webhook deployment and its configuration.
  -w, --with-workload        Re-applies the demonstration/test DaemonSet workload.
EOF
}

while true; do
  case "$1" in
  -h | --help ) show_help; exit 0 ;;
  -r | --recreate-cluster ) RECREATE_CLUSTER=1; shift ;;
  -b | --with-build ) OPT_IN_MODE=1; WITH_BUILD=1; shift ;;
  -d | --with-deploy ) OPT_IN_MODE=1; WITH_DEPLOY=1; shift ;;
  -w | --with-workload ) OPT_IN_MODE=1; WITH_WORKLOAD=1; shift ;;
  -- ) shift; break ;;
  * ) break ;;
  esac
done

if [[ ! -x "$KUSTOMIZE" ]]; then
  echo "ERROR: Make sure kustomize is available at $KUSTOMIZE by running 'make kustomize' or set the KUSTOMIZE environment variable appropriately" >&2
fi

if [[ "$RECREATE_CLUSTER" == 1 ]]; then
  k3d cluster delete "$K3D_CLUSTER"  # This does not error if no cluster found
fi

if ! k3d cluster list "$K3D_CLUSTER" | grep knss >/dev/null 2>&1; then
  echo "Playground K3D cluster not found, creating it ..."
  k3d cluster create "$K3D_CLUSTER"
  k3d node create -c "$K3D_CLUSTER" 6g --memory 6G
  k3d node create -c "$K3D_CLUSTER" 4g --memory 4G
  k3d node create -c "$K3D_CLUSTER" 2g --memory 2G
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.15.3/cert-manager.yaml
fi

_ctx=$(kubectl config get-contexts -o name | grep knss)
if [[ -z "$_ctx" ]]; then
  echo "Cannot find knss kubectl context, aborting" >&2
  exit 1
fi

function kctl() {
    kubectl --context "$_ctx" "$@"
}

cd "${SCRIPT_DIR}/.."

if [[ "$OPT_IN_MODE" == 0 || "$WITH_BUILD" == 1 ]]; then
  make docker-build
  make docker-k3d-load K3D_CLUSTER="${K3D_CLUSTER}"
fi

if [[ "$OPT_IN_MODE" == 0 || "$WITH_DEPLOY" == 1 ]]; then
  make deploy
fi

if [[ "$OPT_IN_MODE" == 0 || "$WITH_WORKLOAD" == 1 ]]; then
  # XXX wait for controller to be available
  kctl apply -f - <<EOF
  apiVersion: apps/v1
  kind: DaemonSet
  metadata:
    name: sleep-daemonset
  spec:
    selector:
      matchLabels:
        app: sleep
    template:
      metadata:
        labels:
          app: sleep
          node-specific-sizing.manomano.tech/enabled: "true"
        annotations:
          node-specific-sizing.manomano.tech/request-memory-fraction: "0.05"
          node-specific-sizing.manomano.tech/limit-memory-fraction: "0.09"
      spec:
        terminationGracePeriodSeconds: 0
        containers:
        - name: sleep-a
          image: alpine
          command:
          - "sleep"
          - "infinity"
          resources:
            requests:
              cpu: 10m
              memory: 100Mi
            limits:
              cpu: 30m
              memory: 200Mi
        - name: sleep-b
          image: alpine
          command:
          - "sleep"
          - "infinity"
          resources:
            requests:
              cpu: 90m
              memory: 900Mi
            limits:
              cpu: 270m
              memory: 1800Mi
EOF
fi

#  nodeResourceBounds:
#    minimums:
#      requests:
#        cpu: "0.01"
#        memory: "0.01"
#      limits:
#        cpu: "0.02"
#        memory: "0.02"
#    maximums:
#      requests:
#        cpu: "0.01"
#        memory: "0.01"
#      limits:
#        cpu: "0.02"
#        memory: "0.02"

if [[ "$OPT_IN_MODE" == 0 || "$WITH_BUILD" == 1 ]]; then
  echo "Forcing deployment 'restart' because we did a rebuild"
  kubectl -n kube-system delete "$(kubectl -n kube-system get -o name pods | grep node)"
fi

echo "Test OK"
