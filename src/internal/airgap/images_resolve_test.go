package airgap_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("OCI chart version resolution", func() {
	DescribeTable("IsConcreteChartVersion",
		func(v string, want bool) {
			Expect(airgap.IsConcreteChartVersion(v)).To(Equal(want))
		},
		Entry("semver patch", "0.9.58", true),
		Entry("semver minor", "0.1.8", true),
		Entry("latest", "latest", true),
		Entry("wildcard star", "0.9.*", false),
		Entry("legacy placeholder", "0.9.x", false),
		Entry("caret constraint", "^0.9.0", false),
		Entry("empty", "", false),
		Entry("star only", "*", false),
	)

	DescribeTable("NormalizeChartConstraint",
		func(in, want string) {
			Expect(airgap.NormalizeChartConstraint(in)).To(Equal(want))
		},
		Entry("empty becomes star", "", "*"),
		Entry("star unchanged", "*", "*"),
		Entry("dot-star unchanged", "0.9.*", "0.9.*"),
		Entry("dot-x to star", "0.9.x", "0.9.*"),
		Entry("dot-X to star", "0.9.X", "0.9.*"),
		Entry("concrete unchanged", "0.9.58", "0.9.58"),
	)

	It("picks the latest tag matching 0.9.*", func() {
		ver, err := airgap.PickLatestMatchingTag([]string{
			"0.8.9", "0.9.41", "0.9.57", "0.9.58", "0.10.0", "latest", "0.9.x",
		}, "0.9.*")
		Expect(err).NotTo(HaveOccurred())
		Expect(ver).To(Equal("0.9.58"))
	})

	It("picks the latest tag matching 0.9.x placeholder form", func() {
		ver, err := airgap.PickLatestMatchingTag([]string{"0.9.1", "0.9.50", "0.9.57"}, "0.9.x")
		Expect(err).NotTo(HaveOccurred())
		Expect(ver).To(Equal("0.9.57"))
	})

	It("returns concrete versions as-is even when skip is set", func() {
		ver, err := airgap.ResolveOCIChartVersion("quay.io/validatedpatterns", "clustergroup", "0.9.41", true)
		Expect(err).NotTo(HaveOccurred())
		Expect(ver).To(Equal("0.9.41"))
	})

	It("refuses wildcards when skip-oci-resolve is set", func() {
		_, err := airgap.ResolveOCIChartVersion("quay.io/validatedpatterns", "clustergroup", "0.9.*", true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("skip-oci-resolve"))
	})
})
