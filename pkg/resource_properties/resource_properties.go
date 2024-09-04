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
	"strings"
)

type ResourceProperty string
type ResourceKind string

const (
	ResourceInvalid    ResourceProperty = "invalid"
	ResourceRequests   ResourceProperty = "requests"
	ResourceLimits     ResourceProperty = "limits"
	ResourcePodMinimum ResourceProperty = "pod-minimum"
	ResourcePodMaximum ResourceProperty = "pod-maximum"

	ResourceFraction ResourceKind = "fraction"
	ResourceQuantity ResourceKind = "quantity"
)

var allValidResourceProperties = []ResourceProperty{ResourceRequests, ResourceLimits, ResourcePodMinimum, ResourcePodMaximum}

type ResourcePropertyBinding struct {
	resourceKind ResourceKind
	resourceProp ResourceProperty
	resourceName corev1.ResourceName
	value        float64
}

func NewBinding(resourceKind ResourceKind, resourceProp ResourceProperty, resourceName corev1.ResourceName, value float64) *ResourcePropertyBinding {
	return &ResourcePropertyBinding{
		resourceKind: resourceKind,
		resourceProp: resourceProp,
		resourceName: resourceName,
		value:        value,
	}
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

func (rpb *ResourcePropertyBinding) SetValue(v float64) {
	rpb.value = v
}

func (rpb *ResourcePropertyBinding) String() string {
	return fmt.Sprintf("%s.%s=%f=%s (%s)", rpb.resourceProp, rpb.resourceName, rpb.value, rpb.HumanValue(), rpb.resourceKind)
}

func appropriateIntegerExponent(n float64, base float64) int {
	if n == 0 {
		return 0
	}
	log := math.Log(n) / math.Log(base)
	truncatedLog := int(math.Trunc(log))
	return (truncatedLog / 3) * 3 // (8 / 3) * 3 = 6, as everybody knows
}

// HumanValue converts from the internal float to a string that looks like
// the usual suffixed representation, i.e. 2G or 200m
func (rpb *ResourcePropertyBinding) HumanValue() string {
	if rpb.resourceKind == ResourceFraction {
		return strconv.FormatFloat(rpb.value, 'f', -1, 64)
	}

	milliQty := rpb.value * 1000
	if milliQty > 10_000 {
		scale := appropriateIntegerExponent(rpb.value, 10.0) // we should be aware if we're not a power of 10 but a power of 2 instead, to preserve Mi/Gi suffixes
		exp := math.Pow10(int(scale))
		return resource.NewScaledQuantity(int64(math.Floor(rpb.value/exp)), resource.Scale(scale)).String()
	} else {
		return resource.NewMilliQuantity(int64(milliQty), resource.DecimalSI).String()
	}
}

func (rpb *ResourcePropertyBinding) PropertyJsonPath(containerIndex int) string {
	return fmt.Sprintf("/spec/containers/%d/resources/%s/%s", containerIndex, string(rpb.resourceProp), rpb.resourceName)
}

// We could technically allow other packages to register or modify the supported annotations. Should we? File an issue!
var supportedAnnotations = map[string]ResourcePropertyBinding{
	"node-specific-sizing.manomano.tech/request-cpu-fraction":    {resourceKind: ResourceFraction, resourceProp: ResourceRequests, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/request-memory-fraction": {resourceKind: ResourceFraction, resourceProp: ResourceRequests, resourceName: corev1.ResourceMemory},
	"node-specific-sizing.manomano.tech/limit-cpu-fraction":      {resourceKind: ResourceFraction, resourceProp: ResourceLimits, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/limit-memory-fraction":   {resourceKind: ResourceFraction, resourceProp: ResourceLimits, resourceName: corev1.ResourceMemory},
	"node-specific-sizing.manomano.tech/minimum-cpu":             {resourceKind: ResourceQuantity, resourceProp: ResourcePodMinimum, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/minimum-memory":          {resourceKind: ResourceQuantity, resourceProp: ResourcePodMinimum, resourceName: corev1.ResourceMemory},
	"node-specific-sizing.manomano.tech/maximum-cpu":             {resourceKind: ResourceQuantity, resourceProp: ResourcePodMaximum, resourceName: corev1.ResourceCPU},
	"node-specific-sizing.manomano.tech/maximum-memory":          {resourceKind: ResourceQuantity, resourceProp: ResourcePodMaximum, resourceName: corev1.ResourceMemory},
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
			err := result.BindPropertyString(supportedBinding.resourceKind, supportedBinding.resourceProp, supportedBinding.resourceName, value)
			if err != nil {
				return err, nil
			}
		}
	}

	return nil, result
}

func (rp *ResourceProperties) String() string {
	sb := strings.Builder{}
	for rp := range rp.All() {
		sb.WriteString(rp.String())
		sb.WriteByte('\n')
	}
	return sb.String()
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
func (rp *ResourceProperties) BindPropertyFloat(kind ResourceKind, prop ResourceProperty, res corev1.ResourceName, value float64) {
	if existing, ok := rp.props[prop][res]; ok {
		existing.value = value
	} else {
		rp.props[prop][res] = &ResourcePropertyBinding{kind, prop, res, value}
	}
}

func parseFraction(value string) (float64, error) {
	result, err := strconv.ParseFloat(value, 64)

	if err != nil {
		return 0, err
	}

	if result <= 0 {
		// We forbid 0 included because it makes no sense as a request or limit
		return 0, fmt.Errorf("%s is not a valid fraction: cannot be <= 0", value)
	}

	if result > 1 {
		return 0, fmt.Errorf("%s is not a valid fraction: cannot be > 1", value)
	}

	return result, nil
}

func parseQuantity(value string) (float64, error) {
	qty, err := resource.ParseQuantity(value)
	if err != nil {
		return 0, err
	}
	return qty.AsApproximateFloat64(), nil
}

// BindPropertyString binds a given resource property to a float value by parsing it from a string.
// The parsing is different whether the kind is a fraction or a quantity:
//   - For fractions, a floating point number between 0 and 1 (excluded) is expected.
//     I'm ~into the idea of support N/M rationals, but that might be purely a curiosity thing.
//   - For quantities, any number that Kubernetes would accept will do. That includes many quantities with SI suffixes, like 100m or 2G
func (rp *ResourceProperties) BindPropertyString(kind ResourceKind, prop ResourceProperty, res corev1.ResourceName, value string) error {
	var err error
	var parsedValue float64

	if kind == ResourceFraction {
		parsedValue, err = parseFraction(value)
	} else {
		parsedValue, err = parseQuantity(value)
	}

	if err != nil {
		return fmt.Errorf("%s cannot be parsed as a %s: %s", value, kind, err)
	}

	rp.BindPropertyFloat(kind, prop, res, parsedValue)
	return nil
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
		rp.BindPropertyFloat(ResourceQuantity, ResourceRequests, name, quantity.AsApproximateFloat64())
	}

	for name, quantity := range reqs.Limits {
		rp.BindPropertyFloat(ResourceQuantity, ResourceLimits, name, quantity.AsApproximateFloat64())
	}
}

// Mul produces new resource properties by multiplying the receiver values by the operand values
// Props unset on either side of the operation are unset on the result rather than set to zero.
//
// The output kind depends on the input kind : multiplying two fractions produces another fraction,
// while any other combination produces a quantity.
func (rp *ResourceProperties) Mul(operand *ResourceProperties) *ResourceProperties {
	result := New()
	for ourBinding := range rp.All() {
		if otherBinding, ok := operand.props[ourBinding.resourceProp][ourBinding.resourceName]; ok {
			kind := ResourceQuantity
			if ourBinding.resourceKind == ResourceFraction && otherBinding.resourceKind == ResourceFraction {
				kind = ResourceFraction
			}
			result.BindPropertyFloat(kind, ourBinding.resourceProp, ourBinding.resourceName, ourBinding.value*otherBinding.value)
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
//
// The ResourceKind algebra is as follows:
// - quantity / quantity => fraction
// - fraction / quantity => quantity (weird way to put things, consider using Mul instead)
// - quantity / fraction => quantity (weird way to put things, consider using Mul instead)
// - fraction / fraction => fraction
func (rp *ResourceProperties) Div(operand *ResourceProperties) *ResourceProperties {
	result := New()
	for ourBinding := range rp.All() {
		otherBinding := operand.props[ourBinding.resourceProp][ourBinding.resourceName]
		kind := ResourceQuantity
		if ourBinding.resourceKind == otherBinding.resourceKind {
			kind = ResourceFraction
		}
		result.BindPropertyFloat(kind, ourBinding.resourceProp, ourBinding.resourceName, ourBinding.value/otherBinding.value)
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
			rp.BindPropertyFloat(request.resourceKind, ResourceRequests, resourceName, limit.Value())
		}
	}
}

// ClampRequestsAndLimits goes over every bound property. If, for any given resourceName, a limit or a requests needs
// to be clamped according to the matching minimum or maximum from userSettings, it will be.
func (rp *ResourceProperties) ClampRequestsAndLimits(userSettings *ResourceProperties) {
	// It could be asserted that the receiver is only made of
	for resourceName := range rp.allResourceNames() {
		minimum, hasMinimum := userSettings.props[ResourcePodMinimum][resourceName]
		maximum, hasMaximum := userSettings.props[ResourcePodMaximum][resourceName]

		for _, prop := range []ResourceProperty{ResourceLimits, ResourceRequests} {
			if bind, isBound := rp.props[prop][resourceName]; isBound {
				if hasMinimum && bind.Value() < minimum.Value() {
					bind.SetValue(minimum.Value())
				}
				if hasMaximum && bind.Value() > maximum.Value() {
					bind.SetValue(maximum.Value())
				}
			}
		}
	}
}
