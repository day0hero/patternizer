package airgap_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("ParseMirrorLog", func() {
	It("extracts skipped oci:// related images", func() {
		tmp, err := os.MkdirTemp("", "mirrorlog-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		logPath := filepath.Join(tmp, "oc-mirror.log")
		Expect(os.WriteFile(logPath, []byte(`
INFO copying operators
WARN 'oci' is not supported in operator catalogs SKIPPING oci://quay.io/kuadrant/wasm-shim:v1.0.0
ERROR related image skipped: oci://ghcr.io/example/sidecar:2.3.4 is not supported
INFO done
`), 0o644)).To(Succeed())

		images, err := airgap.ParseMirrorLog(logPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(images).To(ContainElement("quay.io/kuadrant/wasm-shim:v1.0.0"))
		Expect(images).To(ContainElement("ghcr.io/example/sidecar:2.3.4"))

		snippet := airgap.FormatSkippedImagesYAML(images)
		Expect(snippet).To(ContainSubstring("additionalImages:"))
		Expect(snippet).To(ContainSubstring("quay.io/kuadrant/wasm-shim:v1.0.0"))
	})
})

var _ = Describe("Spoke expansion", func() {
	It("auto-includes managedClusterGroup values when filtering to hub", func() {
		tmp, err := os.MkdirTemp("", "spoke-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		Expect(os.WriteFile(filepath.Join(tmp, "values-global.yaml"), []byte(`
global:
  pattern: spoke-test
main:
  clusterGroupName: hub
  multiSourceConfig:
    enabled: true
    clusterGroupChartVersion: "0.9.1"
`), 0o644)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(tmp, "values-hub.yaml"), []byte(`
clusterGroup:
  name: hub
  namespaces: {}
  subscriptions: {}
  applications: {}
  managedClusterGroups:
    region:
      name: group-one
`), 0o644)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(tmp, "values-group-one.yaml"), []byte(`
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
			ClusterGroups:  []string{"hub"},
			SkipOCIResolve: true,
			WriteValues:    true,
		})
		Expect(err).NotTo(HaveOccurred())

		groupNames := map[string]bool{}
		for _, g := range inv.Groups {
			groupNames[g.Name] = true
		}
		Expect(groupNames["hub"]).To(BeTrue())
		Expect(groupNames["group-one"]).To(BeTrue())

		chartNames := map[string]bool{}
		for _, rc := range inv.RemoteCharts {
			chartNames[rc.Name] = true
		}
		Expect(chartNames["golang-external-secrets"]).To(BeTrue())
	})
})
