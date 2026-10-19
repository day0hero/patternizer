package pattern

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/validatedpatterns/patternizer/internal/types"
)

// getNestedValue is a helper function to get nested values from a map using a path.
func getNestedValue(m map[string]interface{}, path []string) interface{} {
	current := m
	for i, key := range path {
		if i == len(path)-1 {
			return current[key]
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}
	return nil
}

var _ = Describe("ProcessGlobalValues", func() {
	Context("with an existing values file containing custom fields", func() {
		var (
			tempDir    string
			valuesPath string
		)

		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()

			initialValues := map[string]interface{}{
				"global": map[string]interface{}{
					"pattern":     "existing-pattern",
					"customField": "customValue",
					"nestedCustom": map[string]interface{}{
						"key1": "value1",
						"key2": 42,
					},
				},
				"main": map[string]interface{}{
					"clusterGroupName": "custom-cluster-group",
					"multiSourceConfig": map[string]interface{}{
						"enabled":                  false,
						"clusterGroupChartVersion": "1.0.*",
						"customMultiSource":        "customValue",
					},
					"customMainField": "mainCustomValue",
				},
				"customTopLevel": map[string]interface{}{
					"someKey":    "someValue",
					"anotherKey": []string{"item1", "item2"},
				},
			}

			valuesPath = filepath.Join(tempDir, "values-global.yaml")
			initialYaml, err := yaml.Marshal(initialValues)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(valuesPath, initialYaml, 0o644)).To(Succeed())
		})

		It("should preserve all custom fields", func() {
			actualPatternName, clusterGroupName, err := ProcessGlobalValues("new-pattern", tempDir, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(actualPatternName).To(Equal("existing-pattern"))
			Expect(clusterGroupName).To(Equal("custom-cluster-group"))

			processedData, err := os.ReadFile(valuesPath)
			Expect(err).NotTo(HaveOccurred())

			var processedValues map[string]interface{}
			Expect(yaml.Unmarshal(processedData, &processedValues)).To(Succeed())

			tests := []struct {
				path     []string
				expected interface{}
			}{
				{[]string{"global", "pattern"}, "existing-pattern"},
				{[]string{"global", "customField"}, "customValue"},
				{[]string{"global", "nestedCustom", "key1"}, "value1"},
				{[]string{"global", "nestedCustom", "key2"}, 42},
				{[]string{"main", "variant"}, "custom-cluster-group"},
				{[]string{"main", "multiSourceConfig", "enabled"}, false},
				{[]string{"main", "multiSourceConfig", "clusterGroupChartVersion"}, "1.0.*"},
				{[]string{"main", "multiSourceConfig", "customMultiSource"}, "customValue"},
				{[]string{"main", "customMainField"}, "mainCustomValue"},
				{[]string{"customTopLevel", "someKey"}, "someValue"},
			}

			for _, tt := range tests {
				Expect(getNestedValue(processedValues, tt.path)).To(Equal(tt.expected),
					"Field %v should be %v", tt.path, tt.expected)
			}

			// The deprecated clusterGroupName field should be migrated away.
			Expect(getNestedValue(processedValues, []string{"main", "clusterGroupName"})).To(BeNil())

			// Verify array field is preserved
			customTopLevel, ok := processedValues["customTopLevel"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "customTopLevel should be a map")
			anotherKey, ok := customTopLevel["anotherKey"].([]interface{})
			Expect(ok).To(BeTrue(), "anotherKey should be an array")
			Expect(anotherKey).To(Equal([]interface{}{"item1", "item2"}))
		})
	})

	Context("when no existing file exists", func() {
		var tempDir string

		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
		})

		It("should create the file with defaults", func() {
			actualPatternName, clusterGroupName, err := ProcessGlobalValues("test-pattern", tempDir, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(actualPatternName).To(Equal("test-pattern"))
			Expect(clusterGroupName).To(Equal("prod"))

			valuesPath := filepath.Join(tempDir, "values-global.yaml")
			Expect(valuesPath).To(BeAnExistingFile())

			data, err := os.ReadFile(valuesPath)
			Expect(err).NotTo(HaveOccurred())

			var values types.ValuesGlobal
			Expect(yaml.Unmarshal(data, &values)).To(Succeed())

			Expect(values.Global.Pattern).To(Equal("test-pattern"))
			Expect(values.Main.Variant).To(Equal("prod"))
			Expect(values.Main.MultiSourceConfig.Enabled).To(BeTrue())
			Expect(values.Main.MultiSourceConfig.ClusterGroupChartVersion).To(Equal("0.9.*"))
			Expect(values.Global.SecretLoader.Disabled).To(BeTrue())
		})
	})

	Context("with secrets enabled", func() {
		var tempDir string

		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()
		})

		It("should set SecretLoader.Disabled to false", func() {
			actualPatternName, clusterGroupName, err := ProcessGlobalValues("test-pattern", tempDir, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(actualPatternName).To(Equal("test-pattern"))
			Expect(clusterGroupName).To(Equal("prod"))

			valuesPath := filepath.Join(tempDir, "values-global.yaml")
			Expect(valuesPath).To(BeAnExistingFile())

			data, err := os.ReadFile(valuesPath)
			Expect(err).NotTo(HaveOccurred())

			var values types.ValuesGlobal
			Expect(yaml.Unmarshal(data, &values)).To(Succeed())

			Expect(values.Global.Pattern).To(Equal("test-pattern"))
			Expect(values.Global.SecretLoader.Disabled).To(BeFalse())
		})
	})
})

var _ = Describe("ProcessClusterGroupValues", func() {
	Context("with an existing values file containing custom fields", func() {
		var (
			tempDir       string
			valuesPath    string
			oldValuesPath string
		)

		BeforeEach(func() {
			tempDir = GinkgoT().TempDir()

			initialValues := map[string]interface{}{
				"clusterGroup": map[string]interface{}{
					"name":         "prod",
					"isHubCluster": true,
					"namespaces":   []interface{}{"custom-ns1", "custom-ns2"},
					"projects":     []interface{}{"custom-proj1", "custom-proj2"},
					"subscriptions": map[string]interface{}{
						"custom-operator": map[string]interface{}{
							"name":      "custom-operator",
							"namespace": "custom-namespace",
							"channel":   "stable",
							"source":    "community-operators",
						},
					},
					"applications": map[string]interface{}{
						"custom-app": map[string]interface{}{
							"name":           "custom-app",
							"namespace":      "custom-namespace",
							"project":        "custom-project",
							"path":           "custom/path",
							"customAppField": "customAppValue",
						},
					},
					"customClusterField": "customClusterValue",
				},
				"customTopLevel": map[string]interface{}{
					"customKey": "customValue",
				},
			}

			oldValuesPath = filepath.Join(tempDir, "values-prod.yaml")
			valuesPath = filepath.Join(tempDir, "variants", "prod", "values-prod.yaml")
			initialYaml, err := yaml.Marshal(initialValues)
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(oldValuesPath, initialYaml, 0o644)).To(Succeed())
		})

		It("should migrate the legacy root file into the variants folder", func() {
			chartPaths := []string{"charts/app1", "charts/app2"}
			Expect(ProcessClusterGroupValues("test-pattern", "prod", tempDir, chartPaths, false)).To(Succeed())

			Expect(valuesPath).To(BeAnExistingFile())
			Expect(oldValuesPath).NotTo(BeAnExistingFile())
		})

		It("should preserve custom fields", func() {
			chartPaths := []string{"charts/app1", "charts/app2"}
			Expect(ProcessClusterGroupValues("test-pattern", "prod", tempDir, chartPaths, false)).To(Succeed())

			processedData, err := os.ReadFile(valuesPath)
			Expect(err).NotTo(HaveOccurred())

			var processedValues map[string]interface{}
			Expect(yaml.Unmarshal(processedData, &processedValues)).To(Succeed())

			tests := []struct {
				path     []string
				expected interface{}
			}{
				{[]string{"clusterGroup", "name"}, "prod"},
				{[]string{"clusterGroup", "isHubCluster"}, true},
				{[]string{"clusterGroup", "customClusterField"}, "customClusterValue"},
				{[]string{"customTopLevel", "customKey"}, "customValue"},
			}

			for _, tt := range tests {
				Expect(getNestedValue(processedValues, tt.path)).To(Equal(tt.expected),
					"Field %v should be %v", tt.path, tt.expected)
			}

			clusterGroup := processedValues["clusterGroup"].(map[string]interface{})
			namespaces := clusterGroup["namespaces"].(map[string]interface{})
			Expect(namespaces).To(HaveKey("custom-ns1"))
			Expect(namespaces).To(HaveKey("custom-ns2"))
			Expect(namespaces).To(HaveKey("test-pattern"))
		})

		It("should preserve custom application fields", func() {
			chartPaths := []string{"charts/app1", "charts/app2"}
			Expect(ProcessClusterGroupValues("test-pattern", "prod", tempDir, chartPaths, false)).To(Succeed())

			processedData, err := os.ReadFile(valuesPath)
			Expect(err).NotTo(HaveOccurred())

			var processedValues map[string]interface{}
			Expect(yaml.Unmarshal(processedData, &processedValues)).To(Succeed())

			clusterGroup := processedValues["clusterGroup"].(map[string]interface{})
			applications := clusterGroup["applications"].(map[string]interface{})

			customApp := applications["custom-app"].(map[string]interface{})
			Expect(customApp["customAppField"]).To(Equal("customAppValue"))
		})

		It("should preserve custom subscriptions", func() {
			chartPaths := []string{"charts/app1", "charts/app2"}
			Expect(ProcessClusterGroupValues("test-pattern", "prod", tempDir, chartPaths, false)).To(Succeed())

			processedData, err := os.ReadFile(valuesPath)
			Expect(err).NotTo(HaveOccurred())

			var processedValues map[string]interface{}
			Expect(yaml.Unmarshal(processedData, &processedValues)).To(Succeed())

			clusterGroup := processedValues["clusterGroup"].(map[string]interface{})
			subscriptions := clusterGroup["subscriptions"].(map[string]interface{})

			customSub := subscriptions["custom-operator"].(map[string]interface{})
			Expect(customSub["channel"]).To(Equal("stable"))
		})

		It("should add new applications while preserving existing ones", func() {
			chartPaths := []string{"charts/app1", "charts/app2"}
			Expect(ProcessClusterGroupValues("test-pattern", "prod", tempDir, chartPaths, false)).To(Succeed())

			processedData, err := os.ReadFile(valuesPath)
			Expect(err).NotTo(HaveOccurred())

			var processedValues map[string]interface{}
			Expect(yaml.Unmarshal(processedData, &processedValues)).To(Succeed())

			clusterGroup := processedValues["clusterGroup"].(map[string]interface{})
			applications := clusterGroup["applications"].(map[string]interface{})

			Expect(applications).To(HaveKey("app1"))
			Expect(applications).To(HaveKey("app2"))
			Expect(applications).To(HaveKey("custom-app"))
		})
	})
})

var _ = Describe("ProcessGlobalValues variant migration", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	writeGlobalValues := func(content string) string {
		valuesPath := filepath.Join(tempDir, "values-global.yaml")
		Expect(os.WriteFile(valuesPath, []byte(content), 0o644)).To(Succeed())
		return valuesPath
	}

	readMain := func(valuesPath string) map[string]interface{} {
		data, err := os.ReadFile(valuesPath)
		Expect(err).NotTo(HaveOccurred())
		var values map[string]interface{}
		Expect(yaml.Unmarshal(data, &values)).To(Succeed())
		main, ok := values["main"].(map[string]interface{})
		Expect(ok).To(BeTrue())
		return main
	}

	Context("when only the deprecated clusterGroupName is set", func() {
		It("adopts its value as the variant and removes the deprecated field", func() {
			valuesPath := writeGlobalValues("global:\n  pattern: existing\nmain:\n  clusterGroupName: hub\n")

			_, variant, err := ProcessGlobalValues("existing", tempDir, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(variant).To(Equal("hub"))

			main := readMain(valuesPath)
			Expect(main["variant"]).To(Equal("hub"))
			Expect(main).NotTo(HaveKey("clusterGroupName"))
		})
	})

	Context("when both variant and the deprecated clusterGroupName are set", func() {
		It("keeps variant and removes the deprecated field", func() {
			valuesPath := writeGlobalValues("global:\n  pattern: existing\nmain:\n  variant: real\n  clusterGroupName: legacy\n")

			_, variant, err := ProcessGlobalValues("existing", tempDir, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(variant).To(Equal("real"))

			main := readMain(valuesPath)
			Expect(main["variant"]).To(Equal("real"))
			Expect(main).NotTo(HaveKey("clusterGroupName"))
		})
	})
})

var _ = Describe("UsesDeprecatedClusterGroupName", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	It("returns false when values-global.yaml does not exist", func() {
		used, err := UsesDeprecatedClusterGroupName(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(used).To(BeFalse())
	})

	It("returns true when main.clusterGroupName is present", func() {
		Expect(os.WriteFile(filepath.Join(tempDir, "values-global.yaml"),
			[]byte("main:\n  clusterGroupName: hub\n"), 0o644)).To(Succeed())

		used, err := UsesDeprecatedClusterGroupName(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(used).To(BeTrue())
	})

	It("returns false when main.variant is used", func() {
		Expect(os.WriteFile(filepath.Join(tempDir, "values-global.yaml"),
			[]byte("main:\n  variant: hub\n"), 0o644)).To(Succeed())

		used, err := UsesDeprecatedClusterGroupName(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(used).To(BeFalse())
	})
})

var _ = Describe("VariantFolderFileMissing", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	writeGlobal := func(content string) {
		Expect(os.WriteFile(filepath.Join(tempDir, "values-global.yaml"), []byte(content), 0o644)).To(Succeed())
	}

	writeVariantFile := func(variant string) {
		dir := filepath.Join(tempDir, "variants", variant)
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "values-"+variant+".yaml"), []byte("clusterGroup:\n  name: "+variant+"\n"), 0o644)).To(Succeed())
	}

	It("returns missing=false when values-global.yaml does not exist", func() {
		variant, missing, err := VariantFolderFileMissing(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(variant).To(Equal(""))
		Expect(missing).To(BeFalse())
	})

	It("returns missing=true when the variant folder file is absent", func() {
		writeGlobal("main:\n  variant: hub\n")

		variant, missing, err := VariantFolderFileMissing(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(variant).To(Equal("hub"))
		Expect(missing).To(BeTrue())
	})

	It("returns missing=false when the variant folder file exists", func() {
		writeGlobal("main:\n  variant: hub\n")
		writeVariantFile("hub")

		variant, missing, err := VariantFolderFileMissing(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(variant).To(Equal("hub"))
		Expect(missing).To(BeFalse())
	})

	It("resolves the variant from the legacy clusterGroupName field", func() {
		writeGlobal("main:\n  clusterGroupName: legacy\n")

		variant, missing, err := VariantFolderFileMissing(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(variant).To(Equal("legacy"))
		Expect(missing).To(BeTrue())
	})

	It("defaults the variant to prod when none is configured", func() {
		writeGlobal("global:\n  pattern: test\n")

		variant, missing, err := VariantFolderFileMissing(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(variant).To(Equal("prod"))
		Expect(missing).To(BeTrue())
	})
})

var _ = Describe("MissingSingleArgoCD", func() {
	var tempDir string

	BeforeEach(func() {
		tempDir = GinkgoT().TempDir()
	})

	writeGlobal := func(content string) {
		Expect(os.WriteFile(filepath.Join(tempDir, "values-global.yaml"), []byte(content), 0o644)).To(Succeed())
	}

	It("returns false when values-global.yaml does not exist", func() {
		missing, err := MissingSingleArgoCD(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeFalse())
	})

	It("returns false when global.singleArgoCD is set to true", func() {
		writeGlobal("global:\n  pattern: test\n  singleArgoCD: true\n")

		missing, err := MissingSingleArgoCD(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeFalse())
	})

	It("returns false when global.singleArgoCD is set to false", func() {
		writeGlobal("global:\n  pattern: test\n  singleArgoCD: false\n")

		missing, err := MissingSingleArgoCD(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeFalse())
	})

	It("returns true when global exists without singleArgoCD", func() {
		writeGlobal("global:\n  pattern: test\n")

		missing, err := MissingSingleArgoCD(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())
	})

	It("returns true when there is no global section", func() {
		writeGlobal("main:\n  variant: hub\n")

		missing, err := MissingSingleArgoCD(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeTrue())
	})
})
