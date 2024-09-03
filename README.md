# kube-node-specific-sizing

Helps you resize pods created by a DaemonSet depending on the amount of allocatable resources present on the node.

## How to use

1. Add the `node-specific-sizing.manomano.tech/enabled: "true"` label any pod you'd like to size depending on the node.
   Only the `"true"` string works.
   For DaemonSets - the intended use-case - this should therefore go in `spec: metadata: labels:`

2. Override pod CPU/Memory Request/Limit based on node resources using the following annotations.
    - `node-specific-sizing.manomano.tech/request-cpu-fraction: 0.1`
    - `node-specific-sizing.manomano.tech/limit-cpu-fraction: 0.1`
    - `node-specific-sizing.manomano.tech/request-memory-fraction: 0.1`
    - `node-specific-sizing.manomano.tech/limit-memory-fraction: 0.1`

3. Optionally set absolute minimums, maximums and exclusions
   NB: Only pods with the `node-specific-sizing.manomano.tech/enabled: "true"` label will see their resource modified.
   - `node-specific-sizing.manomano.tech/minimum-cpu-request: 0.5`
   - `node-specific-sizing.manomano.tech/minimum-cpu-limit: 0.5`
   - `node-specific-sizing.manomano.tech/maximum-memory-request: 0.5`
   - `node-specific-sizing.manomano.tech/maximum-memory-limit: 0.5`

4. Optionally exclude some containers from dynamic-sizing
    - `node-specific-sizing.manomano.tech/exclude-containers: istio-init,istio-proxy`
    - NOT IMPLEMENTED

5. Take care of the following
    - In some instances, if limit ends up being below request it will be adjusted to be equal to the request.
    - WARNING: We have not tested all cases of partial configuration or weird mish-mashes. 
    - You're safer defining both requests and limits, or just requests if the underlying DaemonSet does not have limits.
    - Having some containers define a request or limit while others do not is unsupported.

## Resource Sizing Algorithm

On principle, the node-specific allocation is per-pod and not per-container - this is to lower the amount of annotations
that we found tedious to maintain.

To achieve this, the updated container requests and limits (from here on out, "tunables") are derived from the pod ones as
follows:

- For each container in the pod, and for each tunable, compute the tunable's relative value per container.
  For any given container, `relative_tunable = container_tunable / (sum(container_tunables) - sum(excluded_container_tunables))` 
- Derive a `pod_tunable_budget = allocatable_tunable_on_node * configured_pod_proportion - sum(excluded_container_tunables)`. This represents the resources that will be given to the pod.
- Clamp `pod_tunable_budget` if minimums and/or maximums are set for that tunable.
- Finally, `new_absolute_tunable = pod_tunable_budget * relative_tunable` spreads the budget around.

Exclusions and clamping notwithstanding, the requests/limits proportions between the different containers do not vary with node specific sizing.

Here's a little example of figuring out `relative_tunables` for memory requests (MR), memory limits (ML), cpu requests (CR) and cpu limits (CL):
~~~
    Memory    Compute
    MR   ML   CR   CL
   ------------------

 C1| 1    2    1    2         //
 C2| 2    2    2    2         // <- Container absolute resource requirements
 C3| 3    5    3    5         //    Input: container_tunable above

 T | 6    9    6    9         // <- Pod total absolute resource requirements
                              //    Intermediate result: sum(container_tunable_values)

PC1| .16  .22  .16  .22       //
PC2| .33  .22  .33  .22       // <- Container relative resource requirements
PC3| .50  .55  .50  .55       //    Output: relative_tunables
~~~

## Development

### Prerequisites

- [git](https://git-scm.com/downloads)
- [go](https://golang.org/dl/) version v1.17+
- [docker](https://docs.docker.com/install/) version 19.03+
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version v1.19+
- [k3d](...) recommended.

### Build & Playground

1. `make build` and `make docker-build`
2. `make deploy` to setup manifests in current context
3. `bin/playground.sh` to setup a K3D playground cluster with a toy daemonset with annotations set
4. `bin/dev_toggle.sh` to reconfigure the K3D playground cluster so that it can reach the webhook server on your workstation, 
    as well as extracting certs from the cluster. This allows you to use the IDE of your choice and try things directly.

### 
