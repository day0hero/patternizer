package airgap_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"gopkg.in/yaml.v3"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("ResolveDisconnectedValues", func() {
	It("derives CatalogSources from ocpVersion and helmRepoUrl from registry", func() {
		cfg, err := airgap.ResolveDisconnectedValues(&airgap.AirgapSpec{
			Registry:   "mirror.example.com:8443",
			OCPVersion: "4.22",
		}, &airgap.BootstrapOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.HelmRepoURL).To(Equal("mirror.example.com:8443/validatedpatterns"))
		Expect(cfg.RedHatCS).To(Equal("cs-redhat-operator-index-v4-22"))
		Expect(cfg.CommunityCS).To(Equal("cs-community-operator-index-v4-22"))
	})

	It("prefers explicit catalogSources over ocpVersion", func() {
		cfg, err := airgap.ResolveDisconnectedValues(&airgap.AirgapSpec{
			Registry:   "mirror.example.com:8443",
			OCPVersion: "4.19",
			CatalogSources: map[string]string{
				"redhat":    "cs-redhat-operator-index-v4-22",
				"community": "cs-community-operator-index-v4-22",
			},
		}, &airgap.BootstrapOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.RedHatCS).To(Equal("cs-redhat-operator-index-v4-22"))
		Expect(cfg.CommunityCS).To(Equal("cs-community-operator-index-v4-22"))
	})
})

var _ = Describe("RenderPatternValues", func() {
	It("updates values-global and variants hub values with disconnected fields", func() {
		tmp := GinkgoT().TempDir()
		patternDir := filepath.Join(tmp, "pattern")
		Expect(os.MkdirAll(filepath.Join(patternDir, "variants", "hub"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(patternDir, "values-global.yaml"), []byte(`
global:
  pattern: test-pattern
main:
  variant: hub
  multiSourceConfig:
    enabled: true
    clusterGroupChartVersion: "0.9.*"
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(patternDir, "variants", "hub", "values-hub.yaml"), []byte(`
clusterGroup:
  name: hub
  subscriptions:
    acm:
      name: advanced-cluster-management
      channel: release-2.16
    eso:
      name: openshift-external-secrets-operator
      channel: stable-v1
`), 0o644)).To(Succeed())

		outDir := filepath.Join(tmp, "out")
		cfg := &airgap.DisconnectedValuesConfig{
			HelmRepoURL: "mirror.example.com:8443/validatedpatterns",
			RedHatCS:    "cs-redhat-operator-index-v4-22",
			CommunityCS: "cs-community-operator-index-v4-22",
		}
		files, err := airgap.RenderPatternValues(patternDir, outDir, cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(2))

		globalRaw, err := os.ReadFile(filepath.Join(patternDir, "values-global.yaml"))
		Expect(err).NotTo(HaveOccurred())
		var global map[string]interface{}
		Expect(yaml.Unmarshal(globalRaw, &global)).To(Succeed())
		main := global["main"].(map[string]interface{})
		msc := main["multiSourceConfig"].(map[string]interface{})
		Expect(msc["helmRepoUrl"]).To(Equal("mirror.example.com:8443/validatedpatterns"))
		Expect(msc["clusterGroupChartVersion"]).To(Equal("0.9.*"))
		Expect(main["patternsOperator"].(map[string]interface{})["source"]).To(Equal("cs-community-operator-index-v4-22"))
		Expect(main["gitops"].(map[string]interface{})["operatorSource"]).To(Equal("cs-redhat-operator-index-v4-22"))

		hubRaw, err := os.ReadFile(filepath.Join(patternDir, "variants", "hub", "values-hub.yaml"))
		Expect(err).NotTo(HaveOccurred())
		var hub map[string]interface{}
		Expect(yaml.Unmarshal(hubRaw, &hub)).To(Succeed())
		subs := hub["clusterGroup"].(map[string]interface{})["subscriptions"].(map[string]interface{})
		Expect(subs["acm"].(map[string]interface{})["source"]).To(Equal("cs-redhat-operator-index-v4-22"))
		Expect(subs["eso"].(map[string]interface{})["source"]).To(Equal("cs-redhat-operator-index-v4-22"))
		Expect(hub["acm"].(map[string]interface{})["mce_operator"].(map[string]interface{})["source"]).To(Equal("cs-redhat-operator-index-v4-22"))

		Expect(filepath.Join(outDir, "backup", "values", "values-global.yaml")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "rendered", "variants", "hub", "values-hub.yaml")).To(BeAnExistingFile())
	})
})
