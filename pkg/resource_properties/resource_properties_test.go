package resource_properties_test

import (
	rps "github.com/ManoManoTech/kubernetes-node-specific-sizing/pkg/resource_properties"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Manipulating resource property bindings", Label("ResourcePropertyBinding"), func() {
	When("the quantity is reasonably large", func() {
		rpa := rps.NewBinding(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 840_000_000)
		rpb := rps.NewBinding(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 342_000_000)
		It("prints with the proper suffix without loss of precision", func(ctx SpecContext) {
			Expect(rpa.HumanValue()).To(Equal("840M"))
			Expect(rpb.HumanValue()).To(Equal("342M"))
		})
	})

	When("accessing and mutating a binding directly", func() {
		It("exposes its resource name, property, and value, and allows overwriting the value", func() {
			rpb := rps.NewBinding(rps.ResourceFraction, rps.ResourceLimits, corev1.ResourceMemory, 0.75)
			Expect(rpb.ResourceName()).To(Equal(corev1.ResourceMemory))
			Expect(rpb.Property()).To(Equal(rps.ResourceLimits))
			Expect(rpb.Value()).To(Equal(0.75))

			rpb.SetValue(0.9)
			Expect(rpb.Value()).To(Equal(0.9))
		})

		It("formats its JSON patch path from a container index", func() {
			rpb := rps.NewBinding(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 1)
			Expect(rpb.PropertyJsonPath(2)).To(Equal("/spec/containers/2/resources/requests/cpu"))
		})
	})
})

var _ = Describe("Binding properties from strings", Label("BindPropertyString"), func() {
	When("binding a fraction", func() {
		It("accepts a value strictly between 0 and 1", func() {
			rp := rps.New()
			Expect(rp.BindPropertyString(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, "0.5")).To(Succeed())
			value, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
			Expect(ok).To(BeTrue())
			Expect(value).To(Equal(0.5))
		})

		It("rejects a value of exactly 0", func() {
			rp := rps.New()
			Expect(rp.BindPropertyString(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, "0")).To(HaveOccurred())
		})

		It("rejects a value greater than 1", func() {
			rp := rps.New()
			Expect(rp.BindPropertyString(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, "1.5")).To(HaveOccurred())
		})

		It("rejects a non-numeric value", func() {
			rp := rps.New()
			Expect(rp.BindPropertyString(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, "not-a-number")).To(HaveOccurred())
		})
	})

	When("binding a quantity", func() {
		It("accepts a Kubernetes-style suffixed value", func() {
			rp := rps.New()
			Expect(rp.BindPropertyString(rps.ResourceQuantity, rps.ResourcePodMinimum, corev1.ResourceCPU, "100m")).To(Succeed())
			value, ok := rp.GetValue(rps.ResourcePodMinimum, corev1.ResourceCPU)
			Expect(ok).To(BeTrue())
			Expect(value).To(BeNumerically("~", 0.1, 1e-9))
		})

		It("accepts a large SI-suffixed value", func() {
			rp := rps.New()
			Expect(rp.BindPropertyString(rps.ResourceQuantity, rps.ResourcePodMaximum, corev1.ResourceMemory, "2G")).To(Succeed())
			value, ok := rp.GetValue(rps.ResourcePodMaximum, corev1.ResourceMemory)
			Expect(ok).To(BeTrue())
			Expect(value).To(BeNumerically("~", 2_000_000_000, 1))
		})

		It("rejects a value Kubernetes cannot parse", func() {
			rp := rps.New()
			Expect(rp.BindPropertyString(rps.ResourceQuantity, rps.ResourcePodMinimum, corev1.ResourceCPU, "not-a-quantity")).To(HaveOccurred())
		})
	})
})

var _ = Describe("Building resource properties from pod annotations", Label("NewFromAnnotations"), func() {
	It("binds every supported annotation it finds", func() {
		err, rp := rps.NewFromAnnotations(map[string]string{
			"node-specific-sizing.manomano.tech/request-cpu-fraction": "0.5",
			"node-specific-sizing.manomano.tech/minimum-cpu":          "100m",
		})
		Expect(err).ToNot(HaveOccurred())

		requestValue, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(requestValue).To(Equal(0.5))

		minimumValue, ok := rp.GetValue(rps.ResourcePodMinimum, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(minimumValue).To(BeNumerically("~", 0.1, 1e-9))
	})

	It("returns an error when a supported annotation has an invalid value", func() {
		err, _ := rps.NewFromAnnotations(map[string]string{
			"node-specific-sizing.manomano.tech/request-cpu-fraction": "not-a-fraction",
		})
		Expect(err).To(HaveOccurred())
	})

	It("returns empty properties when no supported annotation is present", func() {
		err, rp := rps.NewFromAnnotations(map[string]string{
			"some-other-annotation": "value",
		})
		Expect(err).ToNot(HaveOccurred())
		_, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("Merging resource properties", Label("Add"), func() {
	It("sums overlapping bindings and copies over new ones", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 1)

		operand := rps.New()
		operand.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 2)
		operand.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceMemory, 5)

		rp.Add(operand)

		cpuValue, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(cpuValue).To(Equal(3.0))

		memoryValue, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceMemory)
		Expect(ok).To(BeTrue())
		Expect(memoryValue).To(Equal(5.0))
	})
})

var _ = Describe("Importing Kubernetes resource requirements", Label("AddResourceRequirements"), func() {
	It("binds both requests and limits as quantities", func() {
		rp := rps.New()
		rp.AddResourceRequirements(&corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("2"),
			},
		})

		requestValue, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(requestValue).To(Equal(1.0))

		limitValue, ok := rp.GetValue(rps.ResourceLimits, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(limitValue).To(Equal(2.0))
	})
})

var _ = Describe("Multiplying resource properties", Label("Mul"), func() {
	It("produces a fraction when both operands are fractions", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, 0.5)

		operand := rps.New()
		operand.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, 0.2)

		result := rp.Mul(operand)
		value, ok := result.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(BeNumerically("~", 0.1, 1e-9))

		binding := rps.NewBinding(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, value)
		Expect(binding.HumanValue()).To(Equal("0.1"))
	})

	It("produces a quantity when operands are not both fractions", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 0.5)

		operand := rps.New()
		operand.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, 0.2)

		result := rp.Mul(operand)
		value, ok := result.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(BeNumerically("~", 0.1, 1e-9))

		binding := rps.NewBinding(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, value)
		Expect(binding.HumanValue()).To(Equal("100m"))
	})

	It("omits a binding absent from the operand", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceMemory, 0.5)

		operand := rps.New()

		result := rp.Mul(operand)
		_, ok := result.GetValue(rps.ResourceRequests, corev1.ResourceMemory)
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("Dividing resource properties", Label("Div"), func() {
	It("produces a fraction when both operands share the same kind", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 1)

		operand := rps.New()
		operand.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 10)

		result := rp.Div(operand)
		value, ok := result.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(BeNumerically("~", 0.1, 1e-9))
	})

	It("produces a quantity when operands have differing kinds", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceFraction, rps.ResourceRequests, corev1.ResourceCPU, 0.5)

		operand := rps.New()
		operand.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 2)

		result := rp.Div(operand)
		value, ok := result.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(BeNumerically("~", 0.25, 1e-9))

		binding := rps.NewBinding(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, value)
		Expect(binding.HumanValue()).To(Equal("250m"))
	})
})

var _ = Describe("Forcing limits above requests", Label("ForceLimitAboveRequest"), func() {
	It("pulls a request down to the limit when the request exceeds it", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 2.0)
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceLimits, corev1.ResourceCPU, 1.0)

		rp.ForceLimitAboveRequest()

		value, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(1.0))
	})

	It("leaves the request untouched when it does not exceed the limit", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceMemory, 1.0)
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceLimits, corev1.ResourceMemory, 2.0)

		rp.ForceLimitAboveRequest()

		value, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceMemory)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(1.0))
	})

	It("leaves a request with no matching limit untouched", func() {
		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceEphemeralStorage, 1.0)

		rp.ForceLimitAboveRequest()

		value, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceEphemeralStorage)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(1.0))
	})
})

var _ = Describe("Clamping requests and limits to user-defined bounds", Label("ClampRequestsAndLimits"), func() {
	It("raises a value below the minimum", func() {
		userSettings := rps.New()
		userSettings.BindPropertyFloat(rps.ResourceQuantity, rps.ResourcePodMinimum, corev1.ResourceCPU, 0.5)

		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 0.1)

		rp.ClampRequestsAndLimits(userSettings)

		value, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(0.5))
	})

	It("lowers a value above the maximum", func() {
		userSettings := rps.New()
		userSettings.BindPropertyFloat(rps.ResourceQuantity, rps.ResourcePodMaximum, corev1.ResourceCPU, 2.0)

		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceLimits, corev1.ResourceCPU, 5.0)

		rp.ClampRequestsAndLimits(userSettings)

		value, ok := rp.GetValue(rps.ResourceLimits, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(2.0))
	})

	It("leaves a value within bounds untouched", func() {
		userSettings := rps.New()
		userSettings.BindPropertyFloat(rps.ResourceQuantity, rps.ResourcePodMinimum, corev1.ResourceCPU, 0.5)
		userSettings.BindPropertyFloat(rps.ResourceQuantity, rps.ResourcePodMaximum, corev1.ResourceCPU, 2.0)

		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceCPU, 1.0)

		rp.ClampRequestsAndLimits(userSettings)

		value, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceCPU)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(1.0))
	})

	It("leaves a resource with no configured bounds untouched", func() {
		userSettings := rps.New()

		rp := rps.New()
		rp.BindPropertyFloat(rps.ResourceQuantity, rps.ResourceRequests, corev1.ResourceMemory, 100.0)

		rp.ClampRequestsAndLimits(userSettings)

		value, ok := rp.GetValue(rps.ResourceRequests, corev1.ResourceMemory)
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal(100.0))
	})
})
