package airgap_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"gopkg.in/yaml.v3"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("BuildImageSet operator channels", func() {
	It("sets defaultChannel and channels from the values-defined channel", func() {
		inv := &airgap.PatternInventory{
			Operators: []airgap.OperatorRef{
				{
					Name:    "advanced-cluster-management",
					Channel: "release-2.16",
					Catalog: airgap.CatalogRedHat,
				},
				{
					Name:    "patterns-operator",
					Catalog: airgap.CatalogCommunity,
				},
			},
		}
		opts := airgap.Options{}
		isc := airgap.BuildImageSet(inv, &opts)
		Expect(isc.Mirror.Platform).To(BeNil())
		Expect(isc.Mirror.Operators).NotTo(BeEmpty())

		var acm *airgap.OperatorPackage
		for i := range isc.Mirror.Operators {
			for j := range isc.Mirror.Operators[i].Packages {
				pkg := &isc.Mirror.Operators[i].Packages[j]
				if pkg.Name == "advanced-cluster-management" {
					acm = pkg
				}
				if pkg.Name == "patterns-operator" {
					Expect(pkg.DefaultChannel).To(BeEmpty())
					Expect(pkg.Channels).To(BeEmpty())
				}
			}
		}
		Expect(acm).NotTo(BeNil())
		Expect(acm.DefaultChannel).To(Equal("release-2.16"))
		Expect(acm.Channels).To(HaveLen(1))
		Expect(acm.Channels[0].Name).To(Equal("release-2.16"))

		raw, err := yaml.Marshal(isc)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring("defaultChannel: release-2.16"))
	})

	It("includes platform channel when OCPVersion is set", func() {
		inv := &airgap.PatternInventory{}
		opts := airgap.Options{OCPVersion: "4.18"}
		isc := airgap.BuildImageSet(inv, &opts)
		Expect(isc.Mirror.Platform).NotTo(BeNil())
		Expect(isc.Mirror.Platform.Channels).To(HaveLen(1))
		Expect(isc.Mirror.Platform.Channels[0].Name).To(Equal("stable-4.18"))
		Expect(isc.Mirror.Platform.Channels[0].Type).To(Equal("ocp"))
	})
})
