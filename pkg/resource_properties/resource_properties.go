// Package resource_properties provides several helpers to work with kubernetes resource requests and limits,
// as well as other quantities that would be specific to a given kubernetes v1.ResourceName
//
// Unlike kube's APIs, requests and limits are programmatically the same, as well as other quantities, which
// greatly reduces tedium when doing arithmetic on those.
//
// It departs from v1 resource handling by leaning heavily into floats, with the round-trip issues that come
// with it, even though some mitigations are provided.
package resource_properties

import (
	"fmt"
	mapset "github.com/deckarep/golang-set/v2"
	"iter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"strconv"
)

type ResourceProperty string

const (
	ResourceInvalid    ResourceProperty = "invalid"
	ResourceRequests   ResourceProperty = "requests"
	ResourceLimits     ResourceProperty = "limits"
	ResourcePodMinimum ResourceProperty = "pod-minimum"
	ResourcePodMaximum ResourceProperty = "pod-maximum"
)

var allValidResourceProperties = []ResourceProperty{ResourceRequests, ResourceLimits, ResourcePodMinimum, ResourcePodMaximum}

type ResourcePropertyBinding struct {
	resourceProp ResourceProperty
	resourceName corev1.ResourceName
	value        float64
}

func (rpb *ResourcePropertyBinding) ResourceName() corev1.ResourceName {
	return rpb.resourceName
}

func (rpb *ResourcePropertyBinding) Property() ResourceProperty {
	return rpb.resourceProp
}

func (rpb *ResourcePropertyBinding) Value() float64 {
	return rpb.value
}

// HumanValue converts from the internal float to a string that looks like
// the usual suffixed representation, i.e. 2G or 200m
func (rpb *ResourcePropertyBinding) HumanValue() string {
	milliQty := rpb.value * 1000
	if milliQty > 10_000 {
		scale := math.Log10(rpb.value)
		exp := math.Pow10(int(scale))
		return resource.NewScaledQuantity(int64(math.Floor(rpb.value/exp)), resource.Scale(scale)).String()
	} else {
		return resource.NewMilliQuantity(int64(milliQty), resource.BinarySI).String()
	}
}

func (rpb *ResourcePropertyBinding) PropertyJsonPath(containerIndex int) string {
	return fmt.Sprintf("/spec/containers/%d/resources/%s/%s", containerIndex, string(rpb.resourceProp), rpb.resourceName)
}

// We could technically allow other packages to register or modify the supported annotations. Should we? File an issue!
var supportedAnnotations = map[string]ResourcePropertyBinding{
	"node-specific-sizing.manomano.tech/request-cpu-fraction":    {resourceProp: ResourceRequests, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/request-memory-fraction": {resourceProp: ResourceRequests, resourceName: corev1.ResourceMemory},
	"node-specific-sizing.manomano.tech/limit-cpu-fraction":      {resourceProp: ResourceLimits, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/limit-memory-fraction":   {resourceProp: ResourceLimits, resourceName: corev1.ResourceMemory},
	"node-specific-sizing.manomano.tech/minimum-cpu-request":     {resourceProp: ResourcePodMinimum, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/maximum-cpu-request":     {resourceProp: ResourcePodMaximum, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/minimum-memory-request":  {resourceProp: ResourcePodMinimum, resourceName: corev1.ResourceMemory},
	"node-specific-sizing.manomano.tech/maximum-memory-request":  {resourceProp: ResourcePodMaximum, resourceName: corev1.ResourceMemory},
}

type ResourceProperties struct {
	props map[ResourceProperty]map[corev1.ResourceName]*ResourcePropertyBinding
}

func New() *ResourceProperties {
	result := &ResourceProperties{
		props: make(map[ResourceProperty]map[corev1.ResourceName]*ResourcePropertyBinding),
	}

	// Pre-allocate level-1 maps to avoid constantly checking for their presence
	for _, prop := range allValidResourceProperties {
		result.props[prop] = make(map[corev1.ResourceName]*ResourcePropertyBinding)
	}
	return result
}

func NewFromAnnotations(annotations map[string]string) (error, *ResourceProperties) {
	result := New()

	for supportedAnnotation, supportedBinding := range supportedAnnotations {
		if value, ok := annotations[supportedAnnotation]; ok {
			err := result.BindPropertyString(supportedBinding.resourceProp, supportedBinding.resourceName, value)
			if err != nil {
				return err, nil
			}
		}
	}

	return nil, result
}

// GetValue returns (value, true) of an existing binding, or (0, false) for an unbound prop
func (rp *ResourceProperties) GetValue(prop ResourceProperty, res corev1.ResourceName) (float64, bool) {
	if ourBinding, ok := rp.props[prop][res]; ok {
		return ourBinding.value, true
	} else {
		return 0, false
	}
}

// All iterates over all bindings
func (rp *ResourceProperties) All() iter.Seq[*ResourcePropertyBinding] {
	return func(yield func(binding *ResourcePropertyBinding) bool) {
		for _, byProps := range rp.props {
			for _, byResource := range byProps {
				if cont := yield(byResource); !cont {
					break
				}
			}
		}
	}
}

// Bind registers a new binding, potentially removing a pre-existing binding.
// The pass-by-value is intentional.
func (rp *ResourceProperties) Bind(bind ResourcePropertyBinding) {
	rp.props[bind.resourceProp][bind.resourceName] = &bind
}

// BindPropertyFloat binds a given resource property to a float value
func (rp *ResourceProperties) BindPropertyFloat(prop ResourceProperty, res corev1.ResourceName, value float64) {
	if existing, ok := rp.props[prop][res]; ok {
		existing.value = value
	} else {
		rp.props[prop][res] = &ResourcePropertyBinding{prop, res, value}
	}
}

// BindPropertyString binds a given resource property to a float value by parsing it from a string.
func (rp *ResourceProperties) BindPropertyString(prop ResourceProperty, res corev1.ResourceName, value string) error {
	result, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return err
	}

	if result <= 0 {
		// We forbid 0 included because it makes no sense as a request or limit
		return fmt.Errorf("%s is not a valid fraction: cannot be <= 0", value)
	}

	if result > 1 {
		return fmt.Errorf("%s is not a valid fraction: cannot be > 1", value)
	}

	rp.BindPropertyFloat(prop, res, result)

	return nil
}

// IncreaseBoundProperty mutates a bound value by adding an increment to it. Calling IncreaseBoundProperty() for an unbound
// property will bind it to the increment, similarly to BindPropertyFloat.
func (rp *ResourceProperties) IncreaseBoundProperty(prop ResourceProperty, res corev1.ResourceName, increment float64) {
	if bind, ok := rp.props[prop][res]; ok {
		bind.value += increment
	} else {
		rp.props[prop][res] = &ResourcePropertyBinding{prop, res, increment}
	}
}

// ScaleBoundProperty mutates a bound value by multiplying it by a scale factor. Calling ScaleBoundProperty() for an unbound
// property is a no-op.
func (rp *ResourceProperties) ScaleBoundProperty(prop ResourceProperty, res corev1.ResourceName, factor float64) {
	if bind, ok := rp.props[prop][res]; ok {
		bind.value *= factor
	}
}

// Add merges two sets of properties into the receiver, by adding properties values from the added props, creating new
// bindings if needed.
// NB: Unlike the Div and Mul operators, the Add operators works in-place.
func (rp *ResourceProperties) Add(operand *ResourceProperties) {
	for otherBinding := range operand.All() {
		if ourBinding, ok := rp.props[otherBinding.resourceProp][otherBinding.resourceName]; ok {
			ourBinding.value += otherBinding.value
		} else {
			otherBindingCopy := *otherBinding
			rp.props[otherBinding.resourceProp][otherBinding.resourceName] = &otherBindingCopy
		}
	}
}

// AddResourceRequirements merge a Kubernetes ResourceRequirements to the props
func (rp *ResourceProperties) AddResourceRequirements(reqs *corev1.ResourceRequirements) {
	for name, quantity := range reqs.Requests {
		rp.BindPropertyFloat(ResourceRequests, name, quantity.AsApproximateFloat64())
	}

	for name, quantity := range reqs.Limits {
		rp.BindPropertyFloat(ResourceLimits, name, quantity.AsApproximateFloat64())
	}
}

// Mul produces new resource properties by multiplying the receiver values by the operand values
// Props unset on either side of the operation are unset on the result rather than set to zero.
func (rp *ResourceProperties) Mul(operand *ResourceProperties) *ResourceProperties {
	result := New()
	for ourBinding := range rp.All() {
		if otherBinding, ok := operand.props[ourBinding.resourceProp][ourBinding.resourceName]; ok {
			result.BindPropertyFloat(ourBinding.resourceProp, ourBinding.resourceName, ourBinding.value*otherBinding.value)
		}
	}
	return result
}

// Div produces new resource properties by dividing the receiver values by the operand values
//
// Only props defined on the receiver will be used. If no matching prop is defined on the operand,
// this operation will panic, like a division by zero would.
//
// If some props are defined on the operand but not on the receiver, then these props will be absent
// from the result.
func (rp *ResourceProperties) Div(operand *ResourceProperties) *ResourceProperties {
	result := New()
	for ourBinding := range rp.All() {
		otherBinding := operand.props[ourBinding.resourceProp][ourBinding.resourceName]
		result.BindPropertyFloat(ourBinding.resourceProp, ourBinding.resourceName, ourBinding.value/otherBinding.value)
	}
	return result
}

func (rp *ResourceProperties) allResourceNames() iter.Seq[corev1.ResourceName] {
	return func(yield func(corev1.ResourceName) bool) {
		seen := mapset.NewThreadUnsafeSet[corev1.ResourceName]()
		for binding := range rp.All() {
			if !seen.Contains(binding.ResourceName()) {
				seen.Add(binding.ResourceName())
				if keepUp := yield(binding.ResourceName()); !keepUp {
					break
				}
			}
		}
	}
}

// ForceLimitAboveRequest goes over every bound property. If, for any given resourceName, a limit would be below the
// request, it is mutated to be equal to the request instead.
//
// This is - not great - but it's a necessary evil when working with floats and their ever-perplexing rounding oddities.
// We could rework our whole package to be able to work with rational numbers expressed as fractions to mitigate most of
// it, but at some point, node resources will have to be divided.
func (rp *ResourceProperties) ForceLimitAboveRequest() {
	for resourceName := range rp.allResourceNames() {
		request, hasRequest := rp.props[ResourceRequests][resourceName]
		limit, hasLimit := rp.props[ResourceLimits][resourceName]

		if hasRequest && hasLimit && (request.Value() > limit.Value()) {
			// XXX log warning, we shouldn't have to do this but because of float imprecision, we sometimes do
			rp.BindPropertyFloat(ResourceRequests, resourceName, limit.Value())
		}
	}
}
