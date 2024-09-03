package main

import (
	"context"
	"fmt"
	rps "github.com/ManoManoTech/kubernetes-node-specific-sizing/pkg/resource_properties"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/json"
	"math"
)

func computeProportionalResourceRequirements(pod *corev1.Pod) map[string]*rps.ResourceProperties {
	containerResources := make(map[string]*rps.ResourceProperties)
	containerRequirements := make(map[string]*rps.ResourceProperties)

	// Figure out totals first
	totalAbsoluteResourcesRequirements := rps.New()

	for _, ctn := range pod.Spec.Containers {
		cr := rps.New()
		cr.AddResourceRequirements(&ctn.Resources)
		containerResources[ctn.Name] = cr

		totalAbsoluteResourcesRequirements.Add(cr)
	}

	// Then derive proportions by container name
	for _, ctn := range pod.Spec.Containers {
		containerRequirements[ctn.Name] = containerResources[ctn.Name].Div(totalAbsoluteResourcesRequirements)
	}

	return containerRequirements
}

func computePodResourceBudget(fractions *rps.ResourceProperties, node *corev1.Node) *rps.ResourceProperties {
	podResourceBudget := rps.New()
	for prop := range fractions.All() {
		if nodeCapacity, ok := node.Status.Capacity[prop.ResourceName()]; ok {
			qty := nodeCapacity.AsApproximateFloat64()
			podResourceBudget.BindPropertyFloat(prop.Property(), prop.ResourceName(), qty*prop.Value())
		}
	}
	return podResourceBudget
}

// multiplyQuantity is likely to be evil and has unstated, unchecked assumptions about several things.
// This is because the resource.Quantity types are weird when it comes to internal representation,
// and going from and to float64 is made difficult on purpose - at best imprecise, at worst incorrect.
// Regardless, sizing resources is what we're here to do, so sizing resources we shall.
func multiplyQuantity(quantity resource.Quantity, multiplier float64) *resource.Quantity {
	qty := quantity.AsApproximateFloat64() * multiplier
	milliQty := quantity.AsApproximateFloat64() * multiplier * 1000
	if milliQty > 10_000 {
		scale := math.Log10(qty)
		exp := math.Pow10(int(scale))
		return resource.NewScaledQuantity(int64(math.Floor(qty/exp)), resource.Scale(scale))
	} else {
		return resource.NewMilliQuantity(int64(milliQty), resource.BinarySI)
	}
}

func computePodContainerResourceBudget(containersProportionalResourceRequirements map[string]*rps.ResourceProperties, podResourceBudget *rps.ResourceProperties) map[string]*rps.ResourceProperties {
	result := make(map[string]*rps.ResourceProperties)
	for containerName, proportionalResourceRequirements := range containersProportionalResourceRequirements {
		result[containerName] = proportionalResourceRequirements.Mul(podResourceBudget)
		result[containerName].ForceLimitAboveRequest()
	}
	return result
}

func getNodeName(pod *corev1.Pod) (error, string) {
	// We're matching the following exact shape and nothing else
	//
	// spec:
	//  affinity:
	//    nodeAffinity:
	//      requiredDuringSchedulingIgnoredDuringExecution:
	//        nodeSelectorTerms:
	//        - matchFields:
	//          - key: metadata.name
	//            operator: In
	//            values:
	//            - k3d-knss-server-0

	if pod.Spec.Affinity == nil {
		return fmt.Errorf("pod does not have affinity"), ""
	}

	if pod.Spec.Affinity.NodeAffinity == nil {
		return fmt.Errorf("pod does not have affinity.NodeAffinity"), ""
	}

	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return fmt.Errorf("pod does not have affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution"), ""
	}

	if len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		return fmt.Errorf("pod has no terms affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms"), ""
	}

	for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, mf := range term.MatchFields {
			if mf.Key == "metadata.name" && mf.Operator == corev1.NodeSelectorOpIn {
				if len(mf.Values) == 1 {
					return nil, mf.Values[0]
				} else {
					return fmt.Errorf("pod has more than one matching field"), ""
				}
			}
		}
	}

	return fmt.Errorf("no appropriate matchfield for node name extraction"), ""
}

func createPatch(ctx context.Context, pod *corev1.Pod) ([]byte, error) {
	var patch []patchOperation

	zap.L().Debug("Starting patch process")

	err, fractions := rps.NewFromAnnotations(pod.Annotations)
	if err != nil {
		return nil, fmt.Errorf("problem parsing annotations: %w", err)
	}

	var nodes corev1.NodeList
	if err := globalClient.List(ctx, &nodes); err != nil {
		return nil, fmt.Errorf("problem fetching node data: %w", err)
	}

	nodeByName := make(map[string]corev1.Node)
	for _, node := range nodes.Items {
		nodeByName[node.Name] = node
	}

	containersProportionalRequirements := computeProportionalResourceRequirements(pod) // XXX we can probably get away with computing this once, as the proportion may not vary from pod to pod if they have a single controller ...
	err, nodeName := getNodeName(pod)
	if err != nil {
		return nil, fmt.Errorf("problem getting node name: %w", err)
	}
	node, ok := nodeByName[nodeName]

	if !ok {
		return nil, fmt.Errorf("cannot find data for node '%s'", pod.Spec.NodeName)
	}

	zap.L().Debug("containersProportionalRequirements", zap.Any("cPRR", containersProportionalRequirements))

	// We need pod budget = node resources * nssConfig.nodeResourcesFractions
	// When we have pod budget we want pod container budget = podBudget * containersProportionalRequirements
	// Then set values
	podResourceBudget := computePodResourceBudget(fractions, &node)

	zap.L().Debug("podResourceBudget", zap.Any("pRB", *podResourceBudget))

	containersResourceBudget := computePodContainerResourceBudget(containersProportionalRequirements, podResourceBudget)

	zap.L().Debug("containersResourceBudget", zap.Any("cPCRB", containersResourceBudget))

	for i, ctn := range pod.Spec.Containers {
		for binding := range containersResourceBudget[ctn.Name].All() {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  binding.PropertyJsonPath(i),
				Value: binding.HumanValue(),
			})
		}
	}

	if len(patch) > 0 {
		zap.L().Debug(fmt.Sprintf("concluding patch process with %d patches", len(patch)))
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  "/metadata/annotations/node-specific-sizing.manomano.tech~1status",
			Value: fmt.Sprintf("patch_count=%d", len(patch)),
		})
		_, _ = fmt.Printf("%+v\n", patch)
	} else {
		zap.L().Debug("concluding patch process without creating a single patch")
	}

	return json.Marshal(patch)
}
