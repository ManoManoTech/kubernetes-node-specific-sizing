package resource_properties_test

import (
	rps "github.com/ManoManoTech/kubernetes-node-specific-sizing/pkg/resource_properties"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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
})
