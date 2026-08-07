package airgap_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("variants layout discovery", func() {
	It("loads values from variants/<variant>/ when main.variant is set", func() {
		tmp, err := os.MkdirTemp("", "variants-layout-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		Expect(os.MkdirAll(filepath.Join(tmp, "variants", "hub"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "values-global.yaml"), []byte(`
global:
  pattern: variants-pattern
main:
  variant: hub
  multiSourceConfig:
    enabled: true
    clusterGroupChartVersion: "0.9.1"
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "variants", "hub", "values-hub.yaml"), []byte(`
clusterGroup:
  name: hub
  namespaces:
    vault:
  subscriptions:
    acm:
      name: advanced-cluster-management
      channel: release-2.16
  applications: {}
  managedClusterGroups:
    region:
      name: group-one
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "variants", "hub", "values-group-one.yaml"), []byte(`
clusterGroup:
  name: group-one
  namespaces:
    hello:
  subscriptions: {}
  applications:
    demo:
      name: demo
      chart: golang-external-secrets
      chartVersion: 0.1.5
`), 0o644)).To(Succeed())

		out := filepath.Join(tmp, "out")
		inv, err := airgap.Init(airgap.Options{
			PatternDir:     tmp,
			OutputDir:      out,
			SkipOCIResolve: true,
			WriteValues:    true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(inv.Variant).To(Equal("hub"))
		Expect(inv.Groups).NotTo(BeEmpty())

		names := map[string]bool{}
		for _, g := range inv.Groups {
			names[g.Name] = true
		}
		Expect(names["hub"]).To(BeTrue())
		Expect(names["group-one"]).To(BeTrue())
	})
})
