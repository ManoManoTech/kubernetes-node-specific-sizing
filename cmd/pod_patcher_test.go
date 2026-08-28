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
