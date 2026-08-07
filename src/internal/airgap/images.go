package airgap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DiscoverImages runs discovery on a local chart (relative to patternDir).
func DiscoverImages(patternDir, chartPath string) ([]string, []string, error) {
	abs := chartPath
	if !filepath.IsAbs(chartPath) {
		abs = filepath.Join(patternDir, chartPath)
	}
	return DiscoverImagesFromChartDir(abs)
}

// DiscoverImagesFromChartDir discovers container images required to deploy a chart.
// Prefer helm template (respects enabled:false). Parent values are a fallback with
// enabled:false subtrees skipped. Upstream dependency defaults and docker.io short
// names are excluded — Validated Patterns users typically cannot/should not pull those.
func DiscoverImagesFromChartDir(chartDir string) ([]string, []string, error) {
	var warnings []string
	images := map[string]struct{}{}

	templated, warn, err := imagesFromHelmTemplate(chartDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("%s helm template: %v", chartDir, err))
	}
	if warn != "" {
		warnings = append(warnings, warn)
	}
	for _, img := range templated {
		images[img] = struct{}{}
	}

	// Fallback / supplement: parent chart values only (not subchart defaults),
	// skipping components with enabled: false.
	fromValues, err := imagesFromValues(chartDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("%s values scrape: %v", chartDir, err))
	}
	for _, img := range fromValues {
		images[img] = struct{}{}
	}

	return filterRequiredImages(images, warnings), warnings, nil
}

// DiscoverOCIChartImages pulls a remote OCI Helm chart and discovers deploy-time images.
func DiscoverOCIChartImages(helmRepo, chartName, version string, skipNetwork bool) ([]string, []string, error) {
	if skipNetwork {
		return nil, []string{fmt.Sprintf("skipped OCI image discovery for %s (skip-oci-resolve)", chartName)}, nil
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return nil, nil, fmt.Errorf("helm required to discover images for %s", chartName)
	}

	ref := fmt.Sprintf("oci://%s/%s", helmRepo, chartName)
	var warnings []string

	tmp, err := os.MkdirTemp("", "patternizer-chart-"+sanitizeDirName(chartName)+"-")
	if err != nil {
		return nil, warnings, err
	}
	defer os.RemoveAll(tmp)

	args := []string{"pull", ref, "--untar", "--untardir", tmp}
	if version != "" && version != "*" && !strings.Contains(version, "*") {
		args = append(args, "--version", version)
	}
	cmd := exec.Command("helm", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fall back to show-values only (enabled-aware) if pull fails
		showImgs, showErr := imagesFromHelmShowValues(ref, version)
		if showErr != nil {
			return nil, warnings, fmt.Errorf("helm pull %s: %w (%s); show values: %v", ref, err, truncate(string(out), 300), showErr)
		}
		warnings = append(warnings, fmt.Sprintf("%s helm pull failed, using show values only: %v", chartName, err))
		return filterRequiredImages(sliceToSet(showImgs), warnings), warnings, nil
	}

	chartDir := findPulledChartDir(tmp, chartName)
	if chartDir == "" {
		return nil, warnings, fmt.Errorf("pulled chart dir not found for %s", chartName)
	}

	if _, err := os.Stat(filepath.Join(chartDir, "Chart.yaml")); err == nil {
		depCmd := exec.Command("helm", "dependency", "build", chartDir)
		if depOut, depErr := depCmd.CombinedOutput(); depErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s helm dependency build: %v (%s)", chartName, depErr, truncate(string(depOut), 200)))
		}
	}

	pulled, pullWarns, err := DiscoverImagesFromChartDir(chartDir)
	warnings = append(warnings, pullWarns...)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("%s discover: %v", chartName, err))
	}
	return pulled, warnings, nil
}

func imagesFromHelmShowValues(ref, version string) ([]string, error) {
	args := []string{"show", "values", ref}
	if version != "" && version != "*" && !strings.Contains(version, "*") {
		args = append(args, "--version", version)
	}
	cmd := exec.Command("helm", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, truncate(string(out), 300))
	}
	var values map[string]interface{}
	if err := yaml.Unmarshal(out, &values); err != nil {
		return nil, err
	}
	var images []string
	collectImageFields(values, &images)
	return uniqueSorted(images), nil
}

func findPulledChartDir(parent, chartName string) string {
	candidate := filepath.Join(parent, chartName)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(parent, e.Name())
		}
	}
	return ""
}

func imagesFromValues(chartDir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(chartDir, "values.yaml"))
	if err != nil {
		return nil, err
	}
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	var images []string
	collectImageFields(values, &images)
	return images, nil
}

// collectImageFields walks values and records repository+tag / image fields.
// Entire subtrees with enabled: false (or enable: false) are skipped so disabled
// injector/CSI/etc. defaults are not mirrored.
func collectImageFields(v interface{}, out *[]string) {
	switch t := v.(type) {
	case map[string]interface{}:
		if isExplicitlyDisabled(t) {
			return
		}
		repo := stringFromYAML(t["repository"])
		tag := stringFromYAML(t["tag"])
		if repo != "" {
			if tag != "" {
				*out = append(*out, repo+":"+tag)
			} else if img := stringFromYAML(t["image"]); img != "" {
				*out = append(*out, img)
			} else {
				*out = append(*out, repo)
			}
		}
		if img := stringFromYAML(t["image"]); img != "" && strings.Contains(img, "/") {
			*out = append(*out, img)
		}
		for _, child := range t {
			collectImageFields(child, out)
		}
	case []interface{}:
		for _, child := range t {
			collectImageFields(child, out)
		}
	}
}

func isExplicitlyDisabled(m map[string]interface{}) bool {
	for _, key := range []string{"enabled", "enable"} {
		switch v := m[key].(type) {
		case bool:
			if !v {
				return true
			}
		case string:
			if strings.EqualFold(v, "false") {
				return true
			}
		}
	}
	return false
}

func stringFromYAML(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	default:
		return ""
	}
}

func imagesFromHelmTemplate(chartDir string) ([]string, string, error) {
	if _, err := exec.LookPath("helm"); err != nil {
		return nil, "helm not found on PATH; skipping helm template discovery", nil
	}
	cmd := exec.Command("helm", "template", "airgap-discover", chartDir, "--include-crds")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s", err, truncate(string(out), 400))
	}
	return scrapeManifestImages(string(out)), "", nil
}

func scrapeManifestImages(manifest string) []string {
	var images []string
	for _, line := range strings.Split(manifest, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image:") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
		val = strings.Trim(val, `"'`)
		if val == "" || strings.HasPrefix(val, "{{") {
			continue
		}
		images = append(images, val)
	}
	return images
}

func filterRequiredImages(images map[string]struct{}, warnings []string) []string {
	out := make([]string, 0, len(images))
	for img := range images {
		if !isLikelyImage(img) {
			continue
		}
		if !isVPSourceImage(img) {
			continue
		}
		out = append(out, normalizeImagePullSpec(img))
	}
	sort.Strings(out)
	return uniqueSorted(out)
}

func sliceToSet(in []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, s := range in {
		out[s] = struct{}{}
	}
	return out
}

// isVPSourceImage accepts only fully-qualified images from registries Validated Patterns
// actually consumes. Rejects docker.io short names (hashicorp/vault) and docker.io/* extras
// from upstream chart defaults that VP does not deploy.
func isVPSourceImage(img string) bool {
	img = strings.TrimSpace(img)
	if img == "" {
		return false
	}
	// Explicit docker.io rejects (including docker.io/library and docker.io/hashicorp)
	if strings.HasPrefix(img, "docker.io/") {
		return false
	}
	host, _, ok := splitRegistry(img)
	if !ok {
		// Unqualified (e.g. hashicorp/vault:1.21.2) → resolves to docker.io — exclude
		return false
	}
	switch host {
	case "registry.redhat.io",
		"registry.connect.redhat.com",
		"registry.access.redhat.com",
		"quay.io",
		"ghcr.io":
		return true
	default:
		// Allow other fully-qualified registries only if they look intentional (have a dot).
		// Still exclude docker hub aliases.
		if host == "docker.io" || host == "index.docker.io" || host == "registry-1.docker.io" {
			return false
		}
		return strings.Contains(host, ".")
	}
}

func splitRegistry(img string) (host, remainder string, ok bool) {
	// digest or tag after last path component — registry is before first /
	parts := strings.SplitN(img, "/", 2)
	if len(parts) < 2 {
		return "", "", false
	}
	host = parts[0]
	// registry hosts contain a dot or a colon (host:port)
	if strings.Contains(host, ".") || strings.Contains(host, ":") {
		return host, parts[1], true
	}
	return "", "", false
}

func normalizeImagePullSpec(img string) string {
	return strings.TrimSpace(img)
}

func isLikelyImage(s string) bool {
	if s == "" || strings.Contains(s, "{{") || strings.Contains(s, "}}") {
		return false
	}
	if !strings.Contains(s, "/") {
		return false
	}
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
		return false
	}
	return true
}

func uniqueSorted(in []string) []string {
	set := map[string]struct{}{}
	for _, s := range in {
		if isLikelyImage(s) && isVPSourceImage(s) {
			set[normalizeImagePullSpec(s)] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ResolveOCIChartVersion resolves a chartVersion constraint against an OCI Helm repo.
func ResolveOCIChartVersion(helmRepo, chartName, constraint string, skip bool) (string, error) {
	if constraint == "" {
		constraint = "*"
	}
	if !strings.ContainsAny(constraint, "*xX^~><") {
		if _, err := parseLooseVersion(constraint); err == nil {
			return strings.TrimPrefix(constraint, "v"), nil
		}
	}
	if skip {
		return strings.TrimSuffix(strings.ReplaceAll(constraint, "*", "0"), "."), nil
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return "", fmt.Errorf("helm required to resolve %s:%s", chartName, constraint)
	}

	ref := fmt.Sprintf("oci://%s/%s", helmRepo, chartName)
	cmd := exec.Command("helm", "show", "chart", ref, "--version", constraint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("helm show chart %s --version %s: %w (%s)", ref, constraint, err, truncate(string(out), 300))
	}
	var meta struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(out, &meta); err != nil {
		return "", err
	}
	if meta.Version == "" {
		return "", fmt.Errorf("empty version from helm show chart %s", ref)
	}
	return meta.Version, nil
}

func parseLooseVersion(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("empty")
	}
	return v, nil
}
