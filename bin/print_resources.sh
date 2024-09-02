#!/usr/bin/env bash

set -eo pipefail

kubectl -n default get -o json pods | \
    jq -r '.items[] | .status.hostIP as $hip | .spec.containers[] | .name as $n | .resources | "\($hip) - \($n) - REQ \(.requests.memory) - LIM \(.limits.memory)"'
