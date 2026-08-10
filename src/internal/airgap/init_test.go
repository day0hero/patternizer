package airgap_test

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"gopkg.in/yaml.v3"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("Airgap Init", func() {
	var tmpDir string
	var patternDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "airgap-test-")
		Expect(err).NotTo(HaveOccurred())
		patternDir = filepath.Join(tmpDir, "pattern")
		Expect(os.MkdirAll(filepath.Join(patternDir, "charts", "all", "hello-world", "templates"), 0o755)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(patternDir, "values-global.yaml"), []byte(`
global:
  pattern: test-pattern
main:
  clusterGroupName: hub
  multiSourceConfig:
    enabled: true
    clusterGroupChartVersion: "0.9.41"
`), 0o644)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(patternDir, "values-hub.yaml"), []byte(`
clusterGroup:
  name: hub
  namespaces:
    hello-world:
  subscriptions:
    acm:
      name: advanced-cluster-management
      namespace: open-cluster-management
      channel: release-2.14
  applications:
    vault:
      name: vault
      namespace: vault
      chart: hashicorp-vault
      chartVersion: 0.1.6
    hello-world:
      name: hello-world
      namespace: hello-world
      path: charts/all/hello-world
  managedClusterGroups:
    exampleRegion:
      name: group-one
`), 0o644)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(patternDir, "charts", "all", "hello-world", "Chart.yaml"), []byte(`
apiVersion: v2
name: hello-world
version: 0.1.0
`), 0o644)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(patternDir, "charts", "all", "hello-world", "values.yaml"), []byte(`
image:
  repository: registry.access.redhat.com/ubi9/httpd-24
  tag: "1.0"
`), 0o644)).To(Succeed())

		Expect(os.WriteFile(filepath.Join(patternDir, "charts", "all", "hello-world", "templates", "deploy.yaml"), []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello
spec:
  template:
    spec:
      containers:
      - name: httpd
        image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
`), 0o644)).To(Succeed())
	})

	AfterEach(func() {
		_ = os.RemoveAll(tmpDir)
	})

	It("generates imagesetconfig and init artifacts", func() {
		outDir := filepath.Join(tmpDir, "out")
		opts := airgap.Options{
			PatternDir:     patternDir,
			OutputDir:      outDir,
			OCPVersion:     "4.19",
			Registry:       "registry.internal.disconnected.net",
			HelmRepo:       "quay.io/validatedpatterns",
			WriteValues:    true,
			SkipOCIResolve: true,
		}
		inv, err := airgap.Init(&opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(inv.PatternName).To(Equal("test-pattern"))
		Expect(inv.ResolvedDir).To(Equal(patternDir))
		Expect(inv.Operators).NotTo(BeEmpty())

		names := map[string]bool{}
		for _, op := range inv.Operators {
			names[op.Name] = true
		}
		Expect(names["advanced-cluster-management"]).To(BeTrue())
		Expect(names["patterns-operator"]).To(BeTrue())
		Expect(names["openshift-gitops-operator"]).To(BeTrue())
		Expect(names["multicluster-engine"]).To(BeTrue())

		Expect(filepath.Join(outDir, "imagesetconfig.yaml")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "values-disconnected.yaml")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "mirror-commands.sh")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "CHECKLIST.md")).To(BeAnExistingFile())

		raw, err := os.ReadFile(filepath.Join(outDir, "imagesetconfig.yaml"))
		Expect(err).NotTo(HaveOccurred())
		var doc map[string]interface{}
		Expect(yaml.Unmarshal(raw, &doc)).To(Succeed())
		Expect(doc["kind"]).To(Equal("ImageSetConfiguration"))
		Expect(doc["apiVersion"]).To(Equal("mirror.openshift.io/v2alpha1"))

		Expect(string(raw)).To(ContainSubstring("name: advanced-cluster-management"))
		Expect(string(raw)).To(ContainSubstring("defaultChannel: release-2.14"))
		Expect(string(raw)).To(ContainSubstring("name: release-2.14"))

		Expect(inv.LocalCharts).NotTo(BeEmpty())
		chartPath := filepath.Join(patternDir, "charts", "all", "hello-world")
		Expect(inv.LocalCharts[0].Path).To(Equal(chartPath))
		Expect(filepath.IsAbs(inv.LocalCharts[0].Path)).To(BeTrue())
		Expect(inv.LocalCharts[0].Images).To(ContainElement("registry.access.redhat.com/ubi9/httpd-24:1.0"))
		Expect(string(raw)).To(ContainSubstring("path: " + chartPath))

		checklist, err := os.ReadFile(filepath.Join(outDir, "CHECKLIST.md"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(checklist)).To(ContainSubstring("Vault + External Secrets"))
		Expect(string(checklist)).To(ContainSubstring("PATTERN_DISCONNECTED_HOME"))
		Expect(string(checklist)).To(ContainSubstring("group-one"))

		vals, err := os.ReadFile(filepath.Join(outDir, "values-disconnected.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(vals)).To(ContainSubstring("cs-community-operator-index-v4-19"))
		Expect(string(vals)).To(ContainSubstring("registry.internal.disconnected.net/validatedpatterns"))
	})

	It("maps catalogs correctly", func() {
		Expect(airgap.LookupCatalog("patterns-operator")).To(Equal(airgap.CatalogCommunity))
		Expect(airgap.LookupCatalog("advanced-cluster-management")).To(Equal(airgap.CatalogRedHat))
		Expect(airgap.CatalogSourceName(airgap.CatalogRedHat, "4.19")).To(Equal("cs-redhat-operator-index-v4-19"))
		Expect(strings.Contains(airgap.CatalogImage(airgap.CatalogCommunity, "4.19"), "community-operator-index")).To(BeTrue())
	})
})
