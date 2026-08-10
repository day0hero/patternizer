package airgap

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

// IsConcreteChartVersion reports whether v is a concrete OCI tag.
func IsConcreteChartVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if v == "latest" {
		return true
	}
	if strings.ContainsAny(v, "*^~><") {
		return false
	}
	if strings.HasSuffix(strings.ToLower(v), ".x") {
		return false
	}
	return true
}

// NormalizeChartConstraint converts placeholder forms like 0.9.x to 0.9.*.
func NormalizeChartConstraint(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return "*"
	}
	lower := strings.ToLower(constraint)
	if strings.HasSuffix(lower, ".x") {
		return constraint[:len(constraint)-1] + "*"
	}
	if lower == "x" {
		return "*"
	}
	return constraint
}

// ResolveOCIChartVersion resolves a chartVersion constraint to a concrete OCI tag.
func ResolveOCIChartVersion(helmRepo, chartName, constraint string, skip bool) (string, error) {
	constraint = NormalizeChartConstraint(constraint)

	if IsConcreteChartVersion(constraint) {
		return strings.TrimPrefix(constraint, "v"), nil
	}

	if skip {
		return "", fmt.Errorf("cannot resolve %s constraint %q with --skip-oci-resolve; need a concrete version or omit --skip-oci-resolve", chartName, constraint)
	}

	var errs []string

	ver, err := resolveChartVersionHelm(helmRepo, chartName, constraint)
	if err == nil {
		return ver, nil
	}
	errs = append(errs, "helm: "+err.Error())

	ver, err = resolveChartVersionSkopeo(helmRepo, chartName, constraint)
	if err == nil {
		return ver, nil
	}
	errs = append(errs, "skopeo: "+err.Error())

	return "", fmt.Errorf("resolve %s constraint %q: %s", chartName, constraint, strings.Join(errs, "; "))
}

func resolveChartVersionHelm(helmRepo, chartName, constraint string) (string, error) {
	if _, err := exec.LookPath("helm"); err != nil {
		return "", fmt.Errorf("helm not found on PATH")
	}
	ref := fmt.Sprintf("oci://%s/%s", helmRepo, chartName)
	cmd := exec.Command("helm", "show", "chart", ref, "--version", constraint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("show chart %s --version %s: %w (%s)", ref, constraint, err, truncate(string(out), 300))
	}
	var meta struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(out, &meta); err != nil {
		return "", err
	}
	if !IsConcreteChartVersion(meta.Version) {
		return "", fmt.Errorf("empty or non-concrete version %q", meta.Version)
	}
	return meta.Version, nil
}

func resolveChartVersionSkopeo(helmRepo, chartName, constraint string) (string, error) {
	tags, err := listOCITagsSkopeo(helmRepo, chartName)
	if err != nil {
		return "", err
	}
	return PickLatestMatchingTag(tags, constraint)
}

func listOCITagsSkopeo(helmRepo, chartName string) ([]string, error) {
	if _, err := exec.LookPath("skopeo"); err != nil {
		return nil, fmt.Errorf("skopeo not found on PATH")
	}
	ref := fmt.Sprintf("docker://%s/%s", helmRepo, chartName)
	cmd := exec.Command("skopeo", "list-tags", ref)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list-tags %s: %w (%s)", ref, err, truncate(string(out), 300))
	}
	var parsed struct {
		Tags []string `json:"Tags"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse list-tags JSON: %w", err)
	}
	if len(parsed.Tags) == 0 {
		return nil, fmt.Errorf("no tags for %s/%s", helmRepo, chartName)
	}
	return parsed.Tags, nil
}

// PickLatestMatchingTag returns the highest semver tag that satisfies constraint.
func PickLatestMatchingTag(tags []string, constraint string) (string, error) {
	constraint = NormalizeChartConstraint(constraint)
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return "", fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	var matched []*semver.Version
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || !IsConcreteChartVersion(tag) {
			continue
		}
		v, err := semver.NewVersion(strings.TrimPrefix(tag, "v"))
		if err != nil {
			continue
		}
		if c.Check(v) {
			matched = append(matched, v)
		}
	}
	if len(matched) == 0 {
		return "", fmt.Errorf("no tags match constraint %q (%d tags scanned)", constraint, len(tags))
	}
	sort.Sort(semver.Collection(matched))
	return matched[len(matched)-1].Original(), nil
}
