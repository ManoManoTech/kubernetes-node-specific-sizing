# kube-node-specific-sizing

Helps you resize pods created by a DaemonSet depending on the amount of allocatable resources present on the node.

## How to use

1. Add the `node-specific-sizing.manomano.tech/enabled: "true"` label any pod you'd like to size depending on the node.
   Only the `"true"` string works.
   For DaemonSets - the intended use-case - this should therefore go in `spec: metadata: labels:`

2. Override pod CPU/Memory Request/Limit based on node resources using the following annotations.
    - `node-specific-sizing.manomano.tech/request-mcpu-per-node-cpu: 0.1`
    - `node-specific-sizing.manomano.tech/limit-mcpu-per-node-cpu: 0.1`
    - `node-specific-sizing.manomano.tech/request-memory-per-node-memory: 0.1`
    - `node-specific-sizing.manomano.tech/limit-memory-per-node-memory: 0.1`

3. Optionally set absolute minimums, maximums and exclusions
   NB: Only pods with the `node-specific-sizing.manomano.tech/enabled: "true"` label will see their resource modified.
   - `node-specific-sizing.manomano.tech/minimum-cpu-request: 0.5`
   - `node-specific-sizing.manomano.tech/minimum-cpu-limit: 0.5`
   - `node-specific-sizing.manomano.tech/maximum-cpu-request: 0.5`
   - `node-specific-sizing.manomano.tech/maximum-cpu-limit: 0.5`
   

4. Optionally exclude some containers from dynamic-sizing.
    - `node-specific-sizing.manomano.tech/exclude-containers: istio-init,istio-proxy`

5. Take care of the following:
    - In some instances, if limit ends up being below request it will be adjusted to be equal to the request.
    - WARNING: We have not tested all cases of partial configuration or weird mish-mashes. 
    - You're safer defining both requests and limits, or just requests if the underlying DaemonSet does not have limits.
    - Having some containers define a request or limit while others do not is unsupported.

## Resource Sizing Algorithm

Assuming a pod is eligible for dynamic sizing, the mutating webhook computes new resources by following these steps:

- For each container in the pod, and for each tunable [1], compute the tunable's relative value per container.
  For any given container, `relative_tunable = container_tunable / (sum(container_tunables) - sum(excluded_container_tunables))` 
- Derive a `pod_tunable_budget = allocatable_tunable_on_node * configured_pod_proportion - sum(excluded_container_tunables)`. This represents the resources that will be given to the pod.
- Clamp `pod_tunable_budget` if minimums and/or maximums are set for that tunable.
- Finally, `new_absolute_tunable = pod_tunable_budget * relative_tunable` spreads the budget around.

If no containers are excluded from sizing, the requests/limits proportions between the different containers stays the same.

## Development

### Prerequisites

- [git](https://git-scm.com/downloads)
- [go](https://golang.org/dl/) version v1.17+
- [docker](https://docs.docker.com/install/) version 19.03+
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version v1.19+
- [k3d](...) recommended.

## Build and Deploy

1. Build and push docker image:

```bash
make docker-build docker-push IMAGE=quay.io/<your_quayio_username>/node-specific-sizing:latest
```

2. Deploy the kube-node-specific-sizing to kubernetes cluster:

```bash
make deploy IMAGE=quay.io/<your_quayio_username>/node-specific-sizing:latest
```

3. Verify the kube-node-specific-sizing deployment is up and running:

```bash
# kubectl -n node-specific-sizing get pod
# kubectl -n node-specific-sizing get pod
NAME                                READY   STATUS    RESTARTS   AGE
node-specific-sizing-dc75b5d95-spqs7   1/1     Running   0          30s
```
