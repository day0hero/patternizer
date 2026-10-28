package airgap_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"gopkg.in/yaml.v3"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("ExtraConfig", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "airgap-extra-")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		_ = os.RemoveAll(tmpDir)
	})

	It("loads and merges operators, images, and helm.local charts", func() {
		chartDir := filepath.Join(tmpDir, "charts", "demo")
		Expect(os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(`
apiVersion: v2
name: demo
version: 0.1.0
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte(`
image: quay.io/example/demo:1.2.3
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(chartDir, "templates", "deploy.yaml"), []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
spec:
  template:
    spec:
      containers:
      - name: demo
        image: "{{ .Values.image }}"
`), 0o644)).To(Succeed())

		extraPath := filepath.Join(tmpDir, "extra-images.yaml")
		Expect(os.WriteFile(extraPath, []byte(`
operators:
  - catalog: registry.redhat.io/redhat/redhat-operator-index:v4.22
    packages:
      - name: lvms-operator
additionalImages:
  - name: registry.example.com/extra:1.0
helm:
  local:
    - name: demo
      path: `+chartDir+`
    - name:
      path:
`), 0o644)).To(Succeed())

		cfg, err := airgap.LoadExtraConfig(extraPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Operators).To(HaveLen(1))
		Expect(cfg.Operators[0].Packages[0].Name).To(Equal("lvms-operator"))

		inv := &airgap.PatternInventory{}
		opts := &airgap.Options{ExtraConfigPath: extraPath}
		Expect(airgap.ApplyExtraConfig(inv, cfg, opts)).To(Succeed())
		Expect(inv.AdditionalImages).To(ContainElement("registry.example.com/extra:1.0"))
		Expect(inv.AdditionalImages).To(ContainElement("quay.io/example/demo:1.2.3"))
		Expect(inv.LocalCharts).To(HaveLen(1))
		Expect(inv.LocalCharts[0].Path).To(Equal(chartDir))
		Expect(inv.ExtraOperators).To(HaveLen(1))

		isc := airgap.BuildImageSet(inv, &airgap.Options{})
		Expect(isc.Mirror.Platform).To(BeNil())
		Expect(isc.Mirror.Operators).To(HaveLen(1))
		Expect(isc.Mirror.Operators[0].Catalog).To(Equal("registry.redhat.io/redhat/redhat-operator-index:v4.22"))
		Expect(isc.Mirror.Helm).NotTo(BeNil())
		Expect(isc.Mirror.Helm.Local[0].Path).To(Equal(chartDir))

		raw, err := yaml.Marshal(isc)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring("lvms-operator"))
		Expect(string(raw)).To(ContainSubstring("registry.example.com/extra:1.0"))
	})

	It("accepts mirror: wrapped extra config and merges into ImageSet", func() {
		path := filepath.Join(tmpDir, "wrapped.yaml")
		Expect(os.WriteFile(path, []byte(`
mirror:
  operators:
    - catalog: registry.redhat.io/redhat/redhat-operator-index:v4.22
      packages:
        - name: lvms-operator
  additionalImages:
    - name: registry.example.com/wrapped:1
`), 0o644)).To(Succeed())

		cfg, err := airgap.LoadExtraConfig(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Operators).To(HaveLen(1))
		Expect(cfg.AdditionalImages[0].Name).To(Equal("registry.example.com/wrapped:1"))

		isc := &airgap.ImageSetConfiguration{
			Kind:       "ImageSetConfiguration",
			APIVersion: "mirror.openshift.io/v2alpha1",
		}
		airgap.MergeExtraConfigIntoImageSet(isc, cfg)
		Expect(isc.Mirror.Operators).To(HaveLen(1))
		Expect(isc.Mirror.Operators[0].Packages[0].Name).To(Equal("lvms-operator"))
		Expect(isc.Mirror.AdditionalImages).To(ContainElement(airgap.NamedImage{Name: "registry.example.com/wrapped:1"}))
	})

	It("rejects blank additional image names", func() {
		path := filepath.Join(tmpDir, "bad.yaml")
		Expect(os.WriteFile(path, []byte(`
additionalImages:
  - name: ""
`), 0o644)).To(Succeed())
		_, err := airgap.LoadExtraConfig(path)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("additionalImages"))
	})

	It("merges packages into an existing catalog", func() {
		base := []airgap.OperatorCatalog{{
			Catalog: "registry.redhat.io/redhat/redhat-operator-index:v4.22",
			Packages: []airgap.OperatorPackage{
				{Name: "advanced-cluster-management"},
			},
		}}
		extra := []airgap.OperatorCatalog{{
			Catalog: "registry.redhat.io/redhat/redhat-operator-index:v4.22",
			Packages: []airgap.OperatorPackage{
				{Name: "lvms-operator"},
			},
		}}
		merged := airgap.MergeOperatorCatalogs(base, extra)
		Expect(merged).To(HaveLen(1))
		Expect(merged[0].Packages).To(HaveLen(2))
	})
})
