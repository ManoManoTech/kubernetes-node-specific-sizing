#!/usr/bin/env bash

set -eo pipefail
NAMESPACE=${NAMESPACE:-default}
WATCH=${WATCH:-0}
WATCH_DELAY=${WATCH_DELAY:-0.5}
LABEL=${LABEL:-"app=sleep"}

show_help() {
  cat << EOF
  Usage: $0 [--namespace NS] [--watch] [--watch-delay WATCH_DELAY] [--label LABEL]

  Dumps the requests/limits of all pods in a concise way.
  Uses the active kubectl context.

  Options:
  -n, --namespace            Namespace. Default is 'default'

  -w, --watch                Enable 'watch' like mode, which refreshes the output every
                             WATCH_DELAY seconds. Unlike watch, this will print colors just fine.

  -n, --watch-delay          Sets the delay between two prints. Defaults to 0.5
                             Any valid parameters to 'sleep' will work here.

  -l, --label                Label selector for the pods. Defaults to "app=sleep"
EOF
}

while true; do
  case "$1" in
  -h | --help ) show_help; exit 0 ;;
  -n | --namespace ) shift; NAMESPACE=$1 shift ;;
  -w | --watch ) WATCH=1; shift ;;
  -n | --watch-delay ) shift; WATCH_DELAY=$1; shift ;;
  -l | --label ) shift; LABEL=$1; shift ;;
  -- ) shift; break ;;
  * ) break ;;
  esac
done

function print_stats() {
  _pod_json=$(kubectl -n "$NAMESPACE" get -o json -l "$LABEL" pods)
  _len=$(echo "$_pod_json" | jq -r '.items | length - 1')

  if [[ "$_len" == 0 ]]; then
    echo "No pods found in namespace=$NAMESPACE with label=$LABEL" >&2
    exit 1
  fi

  # Display relevant labels / annotations
  _ds_json=$(kubectl -n "$NAMESPACE" get -o json ds -l "$LABEL")
  _annotations=$(echo "$_ds_json" | jq -r '.items[0] | .spec.template.metadata | [.annotations, .labels | to_entries] | flatten | .[] | select(.key | test("node-specific")) | "\(.key): \(.value)"')
  printf "\x1b[38;2;128;192;255m%s \033[0m \n\n\n" "$_annotations"

  # Display per-container resource usage, compact
  for i in $(seq 0 "$_len"); do
    _item_json=$(echo "$_pod_json" | jq -c ".items[$i]")

    _node=$(echo "$_item_json" | jq -r '.spec.nodeName | strings | gsub("\\.[^\\n]*"; "")')
    _containers_count=$(echo "$_item_json" | jq ".spec.containers | length - 1")

    _grey=1
    for j in $(seq 0 "$_containers_count"); do
      _container_resources=$(echo "$_item_json" | \
        jq -rc ".spec.containers[$j] | \"\\(.name) \\(.resources.requests.cpu) \\(.resources.requests.memory) \\(.resources.limits.cpu) \\(.resources.limits.memory)\"")
      readarray -t -d ' ' arr <<< "$_container_resources"

      _name="${arr[0]}"
      _cpu_req="${arr[1]}"
      _mem_req="${arr[2]}"
      _cpu_lim="${arr[3]}"
      _mem_lim="${arr[4]%$'\n'}"

      # shellcheck disable=SC2182
  #    printf "%-60s cpu: req=%-5s lim=%-5s\n" "$_node" "$_cpu_req" "$_cpu_lim"
      [[ $_grey == 1 ]] && echo -ne "\033[48;5;238m"  # ANSI Light Grey Background

      printf "%-30s %-20s mem: req=%-7s lim=%-7s" "$_node" "$_name" "$_mem_req" "$_mem_lim"

      [[ $_grey == 1 ]] && { echo -ne "\033[0m"; _grey=0; } || _grey=1

      printf "\n"
    done
  done
}

if [[ "$WATCH" == 0 ]]; then
  print_stats
else
  while true; do
    _stats=$(print_stats)
    echo -e "\033[H\033[J\033[K${_stats}" # clear visible screen
    sleep "$WATCH_DELAY"
  done
fi
