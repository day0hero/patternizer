package airgap_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("FrameworkImages", func() {
	It("includes utility and imperative containers but not pattern-install", func() {
		imgs := airgap.FrameworkImages("quay.io/validatedpatterns")
		Expect(imgs).To(ConsistOf(
			"quay.io/validatedpatterns/utility-container:latest",
			"quay.io/validatedpatterns/imperative-container:v1",
		))
	})
})
