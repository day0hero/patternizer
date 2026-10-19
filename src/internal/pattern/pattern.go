package pattern

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/validatedpatterns/patternizer/internal/fileutils"
	"github.com/validatedpatterns/patternizer/internal/types"
)

// GetPatternNameAndRepoRoot returns the pattern name and repository root directory.
// The pattern name is derived from the basename of the current working directory.
func GetPatternNameAndRepoRoot() (patternName, repoRoot string, err error) {
	repoRoot, err = os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	patternName = filepath.Base(repoRoot)
	return patternName, repoRoot, nil
}

// deprecatedClusterGroupNameField is the legacy key under 'main' that has been
// replaced by 'main.variant'.
const deprecatedClusterGroupNameField = "clusterGroupName"

// ProcessGlobalValues processes the global values YAML file.
// It returns the pattern name and variant that should be used (from the file if they exist, or the detected/default names).
// If the existing file uses the deprecated 'main.clusterGroupName' field, it is automatically migrated to 'main.variant'.
func ProcessGlobalValues(patternName, repoRoot string, withSecrets bool) (actualPatternName, variant string, err error) {
	globalValuesPath := filepath.Join(repoRoot, "values-global.yaml")
	values := types.NewDefaultValuesGlobal()

	yamlFile, err := os.ReadFile(globalValuesPath)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("failed to read %s: %w", globalValuesPath, err)
	}

	var fileValues types.ValuesGlobal
	if err == nil {
		if err = yaml.Unmarshal(yamlFile, values); err != nil {
			return "", "", fmt.Errorf("failed to unmarshal YAML from %s: %w", globalValuesPath, err)
		}
		if err = yaml.Unmarshal(yamlFile, &fileValues); err != nil {
			return "", "", fmt.Errorf("failed to unmarshal YAML from %s: %w", globalValuesPath, err)
		}
	}

	migrateDeprecatedVariant(values, fileValues.Main.Variant != "")

	if values.Global.Pattern == "" {
		values.Global.Pattern = patternName
	}

	// Set secretLoader.disabled based on withSecrets flag
	// If withSecrets is true, we want secretLoader to be enabled (disabled = false)
	// If withSecrets is false, we want secretLoader to be disabled (disabled = true)
	values.Global.SecretLoader.Disabled = !withSecrets

	if err = fileutils.WriteYAMLWithIndent(values, globalValuesPath); err != nil {
		return "", "", fmt.Errorf("failed to write to %s: %w", globalValuesPath, err)
	}

	return values.Global.Pattern, values.Main.Variant, nil
}

// migrateDeprecatedVariant migrates a legacy 'main.clusterGroupName' field to
// 'main.variant'. An explicit 'main.variant' value (variantExplicit) always wins;
// otherwise the deprecated value is adopted so the existing values-<variant>.yaml
// keeps being used. The deprecated key is always removed so it is not written back.
func migrateDeprecatedVariant(values *types.ValuesGlobal, variantExplicit bool) {
	if values.Main.OtherFields == nil {
		return
	}

	legacy, ok := values.Main.OtherFields[deprecatedClusterGroupNameField]
	if !ok {
		return
	}

	delete(values.Main.OtherFields, deprecatedClusterGroupNameField)

	if !variantExplicit {
		if legacyName, ok := legacy.(string); ok {
			values.Main.Variant = legacyName
		}
	}

	fmt.Printf("Migrated deprecated 'main.clusterGroupName' to 'main.variant' (%s) in values-global.yaml.\n", values.Main.Variant)
}

// UsesDeprecatedClusterGroupName reports whether the pattern's values-global.yaml
// still uses the deprecated 'main.clusterGroupName' field. It returns false without
// error when the file does not exist.
func UsesDeprecatedClusterGroupName(repoRoot string) (bool, error) {
	globalValuesPath := filepath.Join(repoRoot, "values-global.yaml")

	yamlFile, err := os.ReadFile(globalValuesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read %s: %w", globalValuesPath, err)
	}

	var values types.ValuesGlobal
	if err = yaml.Unmarshal(yamlFile, &values); err != nil {
		return false, fmt.Errorf("failed to unmarshal YAML from %s: %w", globalValuesPath, err)
	}

	_, ok := values.Main.OtherFields[deprecatedClusterGroupNameField]
	return ok, nil
}

// variantValuesFileName returns the values file name for a given variant.
func variantValuesFileName(variant string) string {
	return fmt.Sprintf("values-%s.yaml", variant)
}

// variantFolderValuesPath returns the path to a variant's values file in the
// variants folder structure: variants/<variant>/values-<variant>.yaml.
func variantFolderValuesPath(repoRoot, variant string) string {
	return filepath.Join(repoRoot, "variants", variant, variantValuesFileName(variant))
}

// MissingSingleArgoCD reports whether the pattern's values-global.yaml is missing the
// 'global.singleArgoCD' field. It returns false without error when the file does not
// exist (nothing to warn about).
func MissingSingleArgoCD(repoRoot string) (bool, error) {
	globalValuesPath := filepath.Join(repoRoot, "values-global.yaml")

	yamlFile, err := os.ReadFile(globalValuesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read %s: %w", globalValuesPath, err)
	}

	var raw map[string]interface{}
	if err = yaml.Unmarshal(yamlFile, &raw); err != nil {
		return false, fmt.Errorf("failed to unmarshal YAML from %s: %w", globalValuesPath, err)
	}

	global, ok := raw["global"].(map[string]interface{})
	if !ok {
		return true, nil
	}

	_, ok = global["singleArgoCD"]
	return !ok, nil
}

// ProcessClusterGroupValues processes the cluster group values YAML file.
// The file is written to the variants folder structure at
// variants/<variant>/values-<variant>.yaml. If a legacy flat-layout file exists at the
// repository root (values-<variant>.yaml), it is migrated into the variants folder and
// the old file is removed.
func ProcessClusterGroupValues(patternName, variant, repoRoot string, chartPaths []string, useSecrets bool) error {
	newPath := variantFolderValuesPath(repoRoot, variant)
	oldPath := filepath.Join(repoRoot, variantValuesFileName(variant))

	values := types.NewDefaultValuesClusterGroup(patternName, variant, chartPaths, useSecrets)

	oldExists, err := fileExists(oldPath)
	if err != nil {
		return err
	}

	mergeSource := newPath
	if sourceExists, srcErr := fileExists(newPath); srcErr != nil {
		return srcErr
	} else if !sourceExists && oldExists {
		mergeSource = oldPath
	}

	yamlFile, err := os.ReadFile(mergeSource)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", mergeSource, err)
	}

	if err == nil {
		var existingValues types.ValuesClusterGroup
		if err = yaml.Unmarshal(yamlFile, &existingValues); err != nil {
			return fmt.Errorf("failed to unmarshal YAML from %s: %w", mergeSource, err)
		}

		mergeClusterGroupValues(values, &existingValues)
	}

	if err = os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", newPath, err)
	}

	if err = fileutils.WriteYAMLWithIndent(values, newPath); err != nil {
		return fmt.Errorf("failed to write to %s: %w", newPath, err)
	}

	if oldExists {
		if err = fileutils.RemovePathIfExists(oldPath); err != nil {
			return fmt.Errorf("failed to remove legacy %s: %w", oldPath, err)
		}
		fmt.Printf("Migrated %s to variants/%s/.\n", variantValuesFileName(variant), variant)
	}

	return nil
}

// fileExists reports whether a regular file exists at the given path.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to stat %s: %w", path, err)
}

// VariantFolderFileMissing reports whether the pattern is missing the variants folder
// structure file for its configured variant. It resolves the variant from
// 'main.variant' (or the legacy 'main.clusterGroupName', defaulting to "prod") and
// checks for variants/<variant>/values-<variant>.yaml. It returns missing=false when
// values-global.yaml does not exist (nothing to warn about).
func VariantFolderFileMissing(repoRoot string) (variant string, missing bool, err error) {
	globalValuesPath := filepath.Join(repoRoot, "values-global.yaml")

	yamlFile, err := os.ReadFile(globalValuesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("failed to read %s: %w", globalValuesPath, err)
	}

	var values types.ValuesGlobal
	if err = yaml.Unmarshal(yamlFile, &values); err != nil {
		return "", false, fmt.Errorf("failed to unmarshal YAML from %s: %w", globalValuesPath, err)
	}

	variant = values.Main.Variant
	if variant == "" {
		if legacy, ok := values.Main.OtherFields[deprecatedClusterGroupNameField].(string); ok {
			variant = legacy
		}
	}
	if variant == "" {
		variant = "prod"
	}

	exists, err := fileExists(variantFolderValuesPath(repoRoot, variant))
	if err != nil {
		return "", false, err
	}

	return variant, !exists, nil
}

// mergeClusterGroupValues intelligently merges existing values with new defaults
func mergeClusterGroupValues(defaults, existing *types.ValuesClusterGroup) {
	for key, app := range existing.ClusterGroup.Applications {
		defaults.ClusterGroup.Applications[key] = app
	}

	for nsName, nsConfig := range existing.ClusterGroup.Namespaces {
		defaults.ClusterGroup.Namespaces[nsName] = nsConfig
	}

	existingProjectMap := make(map[string]bool)
	for _, proj := range existing.ClusterGroup.Projects {
		existingProjectMap[proj] = true
	}

	mergedProjects := make([]string, 0)
	mergedProjects = append(mergedProjects, existing.ClusterGroup.Projects...)

	for _, proj := range defaults.ClusterGroup.Projects {
		if !existingProjectMap[proj] {
			mergedProjects = append(mergedProjects, proj)
		}
	}

	defaults.ClusterGroup.Projects = mergedProjects

	for key, sub := range existing.ClusterGroup.Subscriptions {
		defaults.ClusterGroup.Subscriptions[key] = sub
	}

	if existing.ClusterGroup.OtherFields != nil {
		for key, value := range existing.ClusterGroup.OtherFields {
			defaults.ClusterGroup.OtherFields[key] = value
		}
	}
	if existing.OtherFields != nil {
		for key, value := range existing.OtherFields {
			defaults.OtherFields[key] = value
		}
	}
}
