package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("could not build scheme: %v", err)
	}
	return scheme
}

func newTestNode(name, cpu, memory string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(memory),
			},
		},
	}
}

func newTestPod(annotations map[string]string, nodeName string, containers ...corev1.Container) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
		Spec: corev1.PodSpec{
			Containers: containers,
		},
	}

	if nodeName != "" {
		pod.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchFields: []corev1.NodeSelectorRequirement{
								{
									Key:      "metadata.name",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{nodeName},
								},
							},
						},
					},
				},
			},
		}
	}

	return pod
}

func TestCreatePatch_HappyPath(t *testing.T) {
	globalClient = fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(newTestNode("node-a", "10", "10Gi")).
		Build()

	pod := newTestPod(
		map[string]string{
			"node-specific-sizing.manomano.tech/request-cpu-fraction":    "0.5",
			"node-specific-sizing.manomano.tech/request-memory-fraction": "0.5",
		},
		"node-a",
		corev1.Container{
			Name: "a",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
		},
		corev1.Container{
			Name: "b",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("3"),
					corev1.ResourceMemory: resource.MustParse("3Gi"),
				},
			},
		},
	)

	patchBytes, err := createPatch(context.Background(), pod)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var ops []patchOperation
	if err := json.Unmarshal(patchBytes, &ops); err != nil {
		t.Fatalf("could not unmarshal patch: %v", err)
	}

	// 2 containers * 2 resources (cpu, memory requests) + 1 trailing status annotation
	if len(ops) != 5 {
		t.Fatalf("expected 5 patch operations, got %d: %+v", len(ops), ops)
	}

	replaceCount := 0
	sawStatus := false
	for _, op := range ops {
		switch {
		case op.Op == "replace" && strings.HasPrefix(op.Path, "/spec/containers/"):
			replaceCount++
		case op.Op == "add" && op.Path == "/metadata/annotations/node-specific-sizing.manomano.tech~1status":
			sawStatus = true
			if op.Value != "patch_count=4" {
				t.Fatalf("expected status annotation to report 4 patches, got %v", op.Value)
			}
		default:
			t.Fatalf("unexpected patch operation: %+v", op)
		}
	}

	if replaceCount != 4 {
		t.Fatalf("expected 4 replace operations, got %d", replaceCount)
	}
	if !sawStatus {
		t.Fatal("expected a trailing status annotation patch operation")
	}
}

func TestCreatePatch_NoMatchingAnnotations(t *testing.T) {
	globalClient = fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(newTestNode("node-a", "10", "10Gi")).
		Build()

	pod := newTestPod(nil, "node-a", corev1.Container{
		Name: "a",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
		},
	})

	patchBytes, err := createPatch(context.Background(), pod)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var ops []patchOperation
	if err := json.Unmarshal(patchBytes, &ops); err != nil {
		t.Fatalf("could not unmarshal patch: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("expected no patch operations, got %d: %+v", len(ops), ops)
	}
}

func TestCreatePatch_InvalidAnnotation(t *testing.T) {
	globalClient = fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		Build()

	pod := newTestPod(map[string]string{
		"node-specific-sizing.manomano.tech/request-cpu-fraction": "not-a-fraction",
	}, "", corev1.Container{Name: "a"})

	_, err := createPatch(context.Background(), pod)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "problem parsing annotations") {
		t.Fatalf("expected annotation parsing error, got %v", err)
	}
}

func TestCreatePatch_MissingAffinity(t *testing.T) {
	globalClient = fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(newTestNode("node-a", "10", "10Gi")).
		Build()

	pod := newTestPod(nil, "", corev1.Container{Name: "a"})

	_, err := createPatch(context.Background(), pod)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "problem getting node name") {
		t.Fatalf("expected node name error, got %v", err)
	}
}

func TestCreatePatch_NodeNotFound(t *testing.T) {
	globalClient = fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(newTestNode("node-a", "10", "10Gi")).
		Build()

	pod := newTestPod(nil, "node-b", corev1.Container{Name: "a"})

	_, err := createPatch(context.Background(), pod)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot find data for node") {
		t.Fatalf("expected node-not-found error, got %v", err)
	}
}
