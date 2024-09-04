#!/usr/bin/env bash

set -eo pipefail

_pod_json=$(kubectl -n default get -o json pods)
_len=$(echo "$_pod_json" | jq -r '.items | length - 1')

# jq -r '.spec.nodeName as $hip | .spec.containers[] | .name as $n | .resources | "\($hip) - \($n) - REQ \(.requests.memory) - LIM \(.limits.memory)"'

for i in `seq 0 "$_len"`; do
  _node=$(echo "$_pod_json" | jq -r ".items[$i] | .spec.nodeName")
  _containers_count=$(echo "$_pod_json" | jq ".items[$i] | .spec.containers | length - 1")

  for j in `seq 0 "$_containers_count"`; do
    _name=$(echo "$_pod_json" | jq -r ".items[$i] | .spec.containers[$j] | .name")
    _cpu_req=$(echo "$_pod_json" | jq -r ".items[$i] | .spec.containers[$j] | .resources.requests.cpu")
    _mem_req=$(echo "$_pod_json" | jq -r ".items[$i] | .spec.containers[$j] | .resources.requests.memory")
    _cpu_lim=$(echo "$_pod_json" | jq -r ".items[$i] | .spec.containers[$j] | .resources.limits.cpu")
    _mem_lim=$(echo "$_pod_json" | jq -r ".items[$i] | .spec.containers[$j] | .resources.limits.memory")

    # shellcheck disable=SC2182
    printf "%-20s cpu: req=%-5s lim=%-5s\n" "$_node" "$_cpu_req" "$_cpu_lim"
    printf "%-20s mem: req=%-5s lim=%-5s\n" "--- $_name" "$_mem_req" "$_mem_lim"
  done
done
