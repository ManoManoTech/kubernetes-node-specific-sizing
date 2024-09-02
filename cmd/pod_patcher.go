package main

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"math"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

//type ResourceConfig string
//
//const (
//	ResourceInvalid  ResourceConfig = "invalid"
//	ResourceRequests ResourceConfig = "requests"
//	ResourceLimits   ResourceConfig = "limits"
//)
//
//type resourceFraction struct {
//	resourceType ResourceConfig
//	resourceName corev1.ResourceName
//	value        float64
//}

type NodeResourceFractions struct {
	Requests corev1.ResourceList `json:"requests"`
	Limits   corev1.ResourceList `json:"limits,omitempty"`
}

func parseFraction(v string) (float64, error) {
	result, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return result, err
	}

	if result <= 0 {
		// We forbid 0 included because it makes no sense as a request or limit
		return result, fmt.Errorf("%s is not a valid fraction: cannot be <= 0", v)
	}

	if result > 1 {
		return result, fmt.Errorf("%s is not a valid fraction: cannot be > 1", v)
	}

	return result, nil
}

func parseAnnotations(annotations map[string]string) (error, *NodeResourceFractions) {
	result := &NodeResourceFractions{
		Requests: corev1.ResourceList{},
		Limits:   corev1.ResourceList{},
	}

	processX := func(intoList corev1.ResourceList, resourceName corev1.ResourceName, annotationString string) error {
		if value, ok := annotations[annotationString]; ok {
			fraction, err := parseFraction(value)
			if err != nil {
				return err
			}
			intoList[resourceName] = *resource.NewScaledQuantity(int64(fraction*1_000_000), resource.Scale(-6))
		}
		return nil
	}

	err := processX(result.Requests, corev1.ResourceCPU, "node-specific-sizing.manomano.tech/request-cpu-per-node-cpu")
	if err != nil {
		return nil, &NodeResourceFractions{}
	}

	err = processX(result.Requests, corev1.ResourceMemory, "node-specific-sizing.manomano.tech/request-memory-per-node-memory")
	if err != nil {
		return nil, &NodeResourceFractions{}
	}

	err = processX(result.Limits, corev1.ResourceCPU, "node-specific-sizing.manomano.tech/limit-cpu-per-node-cpu")
	if err != nil {
		return nil, &NodeResourceFractions{}
	}

	err = processX(result.Limits, corev1.ResourceMemory, "node-specific-sizing.manomano.tech/limit-memory-per-node-memory")
	if err != nil {
		return nil, &NodeResourceFractions{}
	}

	return nil, result
}

//	    Memory    Compute
//	    MR   ML   CR   CL
//	   ------------------
//
//	C1| 1    2    1    2         //
//	C2| 2    2    2    2         // <- Container absolute resource requirements
//	C3| 3    5    3    5         //    (Our input)
//
//	T | 6    9    6    9         // <- Pod total absolute resource requirements
//	                             //    (Intermediate result)
//
// PC1| .16  .22  .16  .22       //
// PC2| .33  .22  .33  .22       // <- Container relative resource requirements
// PC3| .50  .55  .50  .55       //    (Our output, percentage of total per container)
type resourceQuad struct {
	corev1.ResourceRequirements
}

func (rq *resourceQuad) Add(containerRequirements *corev1.ResourceRequirements) {
	for _, resourceName := range []corev1.ResourceName{corev1.ResourceMemory, corev1.ResourceCPU} {
		ourRequests, weHaveRequests := rq.Requests[resourceName]
		containerRequests, containerHasRequests := containerRequirements.Requests[resourceName]

		if weHaveRequests && containerHasRequests {
			ourRequests.Add(containerRequests)
			rq.Requests[resourceName] = ourRequests
		} else if containerHasRequests {
			rq.Requests[resourceName] = containerRequests
		}

		ourLimits, weHaveLimits := rq.Limits[resourceName]
		containerLimits, containerHasLimits := containerRequirements.Limits[resourceName]

		if weHaveLimits && containerHasLimits {
			ourLimits.Add(containerLimits)
			rq.Limits[resourceName] = ourLimits
		} else if containerHasLimits {
			rq.Limits[resourceName] = containerLimits
		}
	}
}

func (rq *resourceQuad) ProportionOf(containerReq *corev1.ResourceRequirements) *ProportionalResourceRequirements {
	result := &ProportionalResourceRequirements{
		Requests: make(map[corev1.ResourceName]float64),
		Limits:   make(map[corev1.ResourceName]float64),
	}

	for _, resourceName := range []corev1.ResourceName{corev1.ResourceMemory, corev1.ResourceCPU} {
		totalRequest, ok := rq.Requests[resourceName]

		totalRequestF := totalRequest.AsApproximateFloat64()
		if ok && totalRequestF != 0 {
			containerRequest := containerReq.Requests[resourceName]
			result.Requests[resourceName] = containerRequest.AsApproximateFloat64() / totalRequestF
		}

		totalLimit, ok := rq.Limits[resourceName]
		totalLimitF := totalLimit.AsApproximateFloat64()
		if ok && totalLimitF != 0 {
			containerLimit := containerReq.Limits[resourceName]
			result.Limits[resourceName] = containerLimit.AsApproximateFloat64() / totalLimitF
		}
	}

	return result
}

type ProportionalResourceRequirements struct {
	Requests map[corev1.ResourceName]float64
	Limits   map[corev1.ResourceName]float64
}

func computeProportionalResourceRequirements(pod *corev1.Pod) map[string]*ProportionalResourceRequirements {
	containersRequirements := make(map[string]*ProportionalResourceRequirements)

	// Figure out totals first
	totalAbsoluteResourcesRequirements := resourceQuad{
		ResourceRequirements: corev1.ResourceRequirements{
			Requests: make(map[corev1.ResourceName]resource.Quantity),
			Limits:   make(map[corev1.ResourceName]resource.Quantity),
		},
	}

	for _, ctn := range pod.Spec.Containers {
		totalAbsoluteResourcesRequirements.Add(&ctn.Resources)
	}

	// Then derive proportions by container name
	for _, ctn := range pod.Spec.Containers {
		containersRequirements[ctn.Name] = totalAbsoluteResourcesRequirements.ProportionOf(&ctn.Resources)
	}

	return containersRequirements
}

func computePodResourceBudget(fractions *NodeResourceFractions, node *corev1.Node) *corev1.ResourceRequirements {
	podResourceBudget := &corev1.ResourceRequirements{
		Limits:   make(corev1.ResourceList),
		Requests: make(corev1.ResourceList),
	}

	nssLimits := fractions.Limits
	nssRequests := fractions.Requests
	for name, nodeResourceQuantity := range node.Status.Capacity {

		if nssResourceLimit, ok := nssLimits[name]; ok {
			podResourceBudget.Limits[name] = *multiplyQuantities(nodeResourceQuantity, nssResourceLimit)
		}

		if nssResourceRequests, ok := nssRequests[name]; ok {
			podResourceBudget.Requests[name] = *multiplyQuantities(nodeResourceQuantity, nssResourceRequests)
		}
	}

	return podResourceBudget
}

// multiplyQuantitues is likely to be even more evil than multiplyQuantity, as the "multiplierQuantity" is not a quantity at all, but a unit-less scalar
func multiplyQuantities(quantity resource.Quantity, multiplierQuantity resource.Quantity) *resource.Quantity {
	multiplier := multiplierQuantity.AsApproximateFloat64()
	return multiplyQuantity(quantity, multiplier)
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

func computePodContainerResourceBudget(containersProportionalResourceRequirements map[string]*ProportionalResourceRequirements, podResourceBudget *corev1.ResourceRequirements) map[string]*corev1.ResourceRequirements {
	podContainerResourceBudget := make(map[string]*corev1.ResourceRequirements)
	for containerName, _ := range containersProportionalResourceRequirements {
		podContainerResourceBudget[containerName] = &corev1.ResourceRequirements{
			Requests: make(corev1.ResourceList),
			Limits:   make(corev1.ResourceList),
		}
	}

	for containerName, proportionalResourceRequirements := range containersProportionalResourceRequirements {
		for _, resourceName := range []corev1.ResourceName{corev1.ResourceMemory, corev1.ResourceCPU} {
			if podResourceBudgetLimits, ok := podResourceBudget.Limits[resourceName]; ok {
				if proportionalResourceRequirementsLimits, ok := proportionalResourceRequirements.Limits[resourceName]; ok {
					podContainerResourceBudget[containerName].Limits[resourceName] = *multiplyQuantity(podResourceBudgetLimits, proportionalResourceRequirementsLimits)
				}
			}

			if podResourceBudgeRequests, ok := podResourceBudget.Requests[resourceName]; ok {
				if proportionalResourceRequirementsRequests, ok := proportionalResourceRequirements.Requests[resourceName]; ok {
					podContainerResourceBudget[containerName].Requests[resourceName] = *multiplyQuantity(podResourceBudgeRequests, proportionalResourceRequirementsRequests)
				}
			}

			// Force request=limit if request>limit - this seems to happen because of some rounding errors.
			// We should probably use fractional proportions instead of floats
			if actualLimit, ok := podContainerResourceBudget[containerName].Limits[resourceName]; ok {
				if actualRequest, ok := podContainerResourceBudget[containerName].Requests[resourceName]; ok {
					limit, _ := actualLimit.AsInt64()
					request, _ := actualRequest.AsInt64()
					if request > limit {
						// XXX log warning, we shouldn't have to do this but because of float imprecision, we sometimes do
						podContainerResourceBudget[containerName].Requests[resourceName] = actualLimit
					}
				}
			}
		}
	}

	return podContainerResourceBudget
}

// NodeSpecificSizingConfigReconciler reconciles a NodeSpecificSizingConfig object
type NodeSpecificSizingConfigReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Manager ctrl.Manager
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

	err, fractions := parseAnnotations(pod.Annotations)
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

	for i, container := range pod.Spec.Containers {
		if v, ok := containersResourceBudget[container.Name].Requests[corev1.ResourceCPU]; ok {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%d/resources/requests/cpu", i),
				Value: v.String(),
			})
		}

		if v, ok := containersResourceBudget[container.Name].Requests[corev1.ResourceMemory]; ok {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%d/resources/requests/memory", i),
				Value: v.String(),
			})
		}

		if v, ok := containersResourceBudget[container.Name].Limits[corev1.ResourceCPU]; ok {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%d/resources/limits/cpu", i),
				Value: v.String(),
			})
		}

		if v, ok := containersResourceBudget[container.Name].Limits[corev1.ResourceMemory]; ok {
			patch = append(patch, patchOperation{
				Op:    "replace",
				Path:  fmt.Sprintf("/spec/containers/%d/resources/limits/memory", i),
				Value: v.String(),
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
