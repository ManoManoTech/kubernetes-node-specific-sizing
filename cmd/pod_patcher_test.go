package main

import (
	"testing"

	rps "github.com/ManoManoTech/kubernetes-node-specific-sizing/pkg/resource_properties"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGetNodeName_HappyPath(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchFields: []corev1.NodeSelectorRequirement{
									{
										Key:      "metadata.name",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"k3d-knss-server-0"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err, nodeName := getNodeName(pod)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if nodeName != "k3d-knss-server-0" {
		t.Fatalf("expected node name %q, got %q", "k3d-knss-server-0", nodeName)
	}
}

func TestGetNodeName_MissingAffinity(t *testing.T) {
	pod := &corev1.Pod{}

	err, nodeName := getNodeName(pod)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if nodeName != "" {
		t.Fatalf("expected empty node name, got %q", nodeName)
	}
}

func TestMultiplyQuantity_SmallValueStaysMilli(t *testing.T) {
	result := multiplyQuantity(resource.MustParse("100m"), 2)
	if result.String() != "200m" {
		t.Fatalf("expected 200m, got %s", result.String())
	}
}

func TestMultiplyQuantity_LargeValueScales(t *testing.T) {
	result := multiplyQuantity(resource.MustParse("4"), 3)
	if result.String() != "10" {
		t.Fatalf("expected 10, got %s", result.String())
	}
}

func TestComputePodResourceBudget(t *testing.T) {
	node := &corev1.Node{
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10"),
				corev1.ResourceMemory: resource.MustParse("10Gi"),
			},
		},
	}

	userSettings := rps.New()
	userSettings.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, 0.5)
	userSettings.BindPropertyFloat(rps.ResourceQuantity, rps.ResourcePodMinimum, corev1.ResourceCPU, 6)
	userSettings.BindPropertyFloat(rps.ResourceFraction, rps.ResourceLimits, corev1.ResourceMemory, 0.5)
	userSettings.BindPropertyFloat(rps.ResourceQuantity, rps.ResourcePodMaximum, corev1.ResourceMemory, 1_000_000_000)

	budget := computePodResourceBudget(userSettings, node)

	cpuValue, ok := budget.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
	if !ok {
		t.Fatal("expected a bound cpu request budget")
	}
	if cpuValue != 6 {
		t.Fatalf("expected cpu request budget to be clamped up to the minimum of 6, got %f", cpuValue)
	}

	memoryValue, ok := budget.GetValue(rps.ResourceLimits, corev1.ResourceMemory)
	if !ok {
		t.Fatal("expected a bound memory limit budget")
	}
	if memoryValue != 1_000_000_000 {
		t.Fatalf("expected memory limit budget to be clamped down to the maximum of 1e9, got %f", memoryValue)
	}
}

func TestComputePodContainerResourceBudget(t *testing.T) {
	containersProportionalResourceRequirements := make(map[string]*rps.ResourceProperties)

	// 'a' keeps a normal request/limit ratio: multiplication alone should apply, no correction needed.
	a := rps.New()
	a.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, 0.25)
	a.BindPropertyFloat(rps.ResourceFraction, rps.ResourceLimits, corev1.ResourceCPU, 0.25)
	containersProportionalResourceRequirements["a"] = a

	// 'b' has a much higher proportional request than limit, so after multiplying by the pod budget its
	// request will exceed its limit and ForceLimitAboveRequest must pull it back down.
	b := rps.New()
	b.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, 0.75)
	b.BindPropertyFloat(rps.ResourceFraction, rps.ResourceLimits, corev1.ResourceCPU, 0.1)
	containersProportionalResourceRequirements["b"] = b

	podResourceBudget := rps.New()
	podResourceBudget.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 4)
	podResourceBudget.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceLimits, corev1.ResourceCPU, 8)

	result := computePodContainerResourceBudget(containersProportionalResourceRequirements, podResourceBudget)

	aRequest, ok := result["a"].GetValue(rps.ResourceRequests, corev1.ResourceCPU)
	if !ok {
		t.Fatal("expected container 'a' to have a bound cpu request budget")
	}
	if aRequest != 1.0 {
		t.Fatalf("expected container 'a' cpu request budget to be 1.0 (0.25 * 4), got %f", aRequest)
	}

	bRequest, ok := result["b"].GetValue(rps.ResourceRequests, corev1.ResourceCPU)
	if !ok {
		t.Fatal("expected container 'b' to have a bound cpu request budget")
	}
	if bRequest != 0.8 {
		t.Fatalf("expected container 'b' cpu request budget to be forced down to its limit of 0.8 (0.1 * 8), got %f", bRequest)
	}
}

func TestComputeProportionalResourceRequirements_HappyPath(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "a",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
				{
					Name: "b",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("3"),
						},
					},
				},
			},
		},
	}

	result := computeProportionalResourceRequirements(pod)

	if len(result) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(result))
	}

	aFraction, ok := result["a"].GetValue(rps.ResourceRequests, corev1.ResourceCPU)
	if !ok {
		t.Fatal("expected container 'a' to have a bound cpu request fraction")
	}
	if aFraction != 0.25 {
		t.Fatalf("expected container 'a' cpu request fraction to be 0.25, got %f", aFraction)
	}

	bFraction, ok := result["b"].GetValue(rps.ResourceRequests, corev1.ResourceCPU)
	if !ok {
		t.Fatal("expected container 'b' to have a bound cpu request fraction")
	}
	if bFraction != 0.75 {
		t.Fatalf("expected container 'b' cpu request fraction to be 0.75, got %f", bFraction)
	}
}
