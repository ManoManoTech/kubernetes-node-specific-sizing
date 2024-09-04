package resource_properties_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestResourceProperties(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ResourceProperties Suite")
}
