package airgap

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValuesSecretFile is the subset of values-secret.yaml used by airgap bootstrap.
// The top-level airgap key is ignored by Vault load-secrets.
type ValuesSecretFile struct {
	Version string      `yaml:"version"`
	Airgap  *AirgapSpec `yaml:"airgap,omitempty"`
}

// AirgapSpec maps hostnames, CA files, and registry credentials for disconnected install.
type AirgapSpec struct {
	ArgocdNamespace           string             `yaml:"argocdNamespace,omitempty"`
	PatternsOperatorNamespace string             `yaml:"patternsOperatorNamespace,omitempty"`
	Certificates              []AirgapNamedBlock `yaml:"certificates,omitempty"`
	Registries                []AirgapNamedBlock `yaml:"registries,omitempty"`

	// Disconnected values rendering (values-global.yaml / variants/*/values-*.yaml).
	Registry         string            `yaml:"registry,omitempty"`         // e.g. mirror.example.com:8443
	HelmRepoURL      string            `yaml:"helmRepoUrl,omitempty"`      // override; default <registry>/validatedpatterns
	OCPVersion       string            `yaml:"ocpVersion,omitempty"`       // e.g. 4.22 — derives CatalogSource names
	CatalogSources   map[string]string `yaml:"catalogSources,omitempty"`   // keys: redhat, community, marketplace
	DiscoverCatalogs bool              `yaml:"discoverCatalogs,omitempty"` // query oc for CatalogSources
	RenderValues     *bool             `yaml:"renderValues,omitempty"`     // default true when pattern dir set
}

// WantsRenderValues reports whether bootstrap should rewrite pattern values files.
func (s *AirgapSpec) WantsRenderValues() bool {
	if s == nil {
		return false
	}
	if s.RenderValues != nil {
		return *s.RenderValues
	}
	return s.Registry != "" || s.HelmRepoURL != "" || s.OCPVersion != "" || len(s.CatalogSources) > 0 || s.DiscoverCatalogs
}

// AirgapNamedBlock mirrors values-secret secret entries (name + fields with value/path).
type AirgapNamedBlock struct {
	Name   string        `yaml:"name"`
	Fields []AirgapField `yaml:"fields"`
}

// AirgapField is a values-secret-style field (literal value or file path).
type AirgapField struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value,omitempty"`
	Path  string `yaml:"path,omitempty"`
}

// ResolvedCertificate is a certificate entry after resolving fields.
type ResolvedCertificate struct {
	Name         string
	Host         string
	CAPath       string
	ClusterProxy bool
	ArgocdTLS    bool
}

// ResolvedRegistry is a registry credential entry after resolving fields.
type ResolvedRegistry struct {
	Name             string
	URL              string
	DockerConfigPath string
	Username         string
	PasswordPath     string
	Password         string
}

// BootstrapOptions configures generate-only unified airgap trust artifacts.
type BootstrapOptions struct {
	PatternDir       string
	PatternName      string
	SecretsFile      string
	OutputDir        string
	Namespace        string // override airgap.argocdNamespace
	RepoServer       string
	DiscoverCatalogs bool // CLI override: query oc for CatalogSources
	SkipRenderValues bool // CLI override: do not rewrite pattern values
	Registry         string
	OCPVersion       string
}

// BootstrapResult summarizes generated bootstrap artifacts.
type BootstrapResult struct {
	OutputDir       string
	SecretsFile     string
	ArgocdNamespace string
	ClusterProxyCAs int
	ArgocdTLSHosts  int
	Registries      int
	RenderedFiles   []string
	HelmRepoURL     string
	RedHatCS        string
	CommunityCS     string
}

// ResolveValuesSecretPath picks the values-secret file the same way VP install does.
// Order: explicit path, ~/values-secret-<pattern>.yaml, <pattern>/values-secret.yaml,
// <pattern>/values-secret.yaml.template.
func ResolveValuesSecretPath(patternDir, patternName, explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		p, err := expandPath(explicit)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("secrets file %s: %w", p, err)
		}
		return p, nil
	}

	home, _ := os.UserHomeDir()
	if patternName != "" && home != "" {
		candidate := filepath.Join(home, "values-secret-"+patternName+".yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	if patternDir != "" {
		for _, name := range []string{"values-secret.yaml", "values-secret.yaml.template"} {
			candidate := filepath.Join(patternDir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("no values-secret file found (tried --secrets-file, ~/values-secret-<pattern>.yaml, values-secret.yaml[.template] under pattern dir)")
}

// LoadAirgapSpec parses the airgap section from a values-secret file.
func LoadAirgapSpec(path string) (*AirgapSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc ValuesSecretFile
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Airgap == nil {
		return nil, fmt.Errorf("%s has no top-level airgap: section (see values-secret.yaml.template comments)", path)
	}
	return doc.Airgap, nil
}

// ResolveCertificates resolves certificate blocks from the airgap spec.
func ResolveCertificates(spec *AirgapSpec) ([]ResolvedCertificate, error) {
	var out []ResolvedCertificate
	for _, block := range spec.Certificates {
		fields, err := resolveFieldMap(block.Fields)
		if err != nil {
			return nil, fmt.Errorf("certificate %q: %w", block.Name, err)
		}
		caPath := fields["ca"]
		if caPath == "" {
			return nil, fmt.Errorf("certificate %q: field ca (path or value) is required", block.Name)
		}
		// ca may be given as path= or as value= containing a path; LoadCAPEM needs a filesystem path.
		if _, err := os.Stat(caPath); err != nil {
			return nil, fmt.Errorf("certificate %q: ca file %s: %w", block.Name, caPath, err)
		}
		host := fields["host"]
		cp := parseBoolField(fields["clusterproxy"])
		at := parseBoolField(fields["argocdtls"])
		if !cp && !at {
			// Default: both, matching common airgap needs.
			cp, at = true, true
		}
		if at && host == "" {
			return nil, fmt.Errorf("certificate %q: field host is required when argocdTLS is true", block.Name)
		}
		out = append(out, ResolvedCertificate{
			Name:         block.Name,
			Host:         host,
			CAPath:       caPath,
			ClusterProxy: cp,
			ArgocdTLS:    at,
		})
	}
	return out, nil
}

// ResolveRegistries resolves registry credential blocks from the airgap spec.
func ResolveRegistries(spec *AirgapSpec) ([]ResolvedRegistry, error) {
	var out []ResolvedRegistry
	for _, block := range spec.Registries {
		fields, err := resolveFieldMap(block.Fields)
		if err != nil {
			return nil, fmt.Errorf("registry %q: %w", block.Name, err)
		}
		url := fields["url"]
		if url == "" {
			return nil, fmt.Errorf("registry %q: field url is required", block.Name)
		}
		r := ResolvedRegistry{
			Name:             block.Name,
			URL:              url,
			DockerConfigPath: fields["dockerconfig"],
			Username:         fields["username"],
		}
		if passField := fields["password"]; passField != "" {
			if _, err := os.Stat(passField); err == nil {
				r.PasswordPath = passField
			} else {
				r.Password = passField
			}
		}
		if r.DockerConfigPath == "" && r.Username == "" {
			return nil, fmt.Errorf("registry %q: provide dockerconfig path or username (+ password path/value)", block.Name)
		}
		out = append(out, r)
	}
	return out, nil
}

func resolveFieldMap(fields []AirgapField) (map[string]string, error) {
	out := map[string]string{}
	for _, f := range fields {
		key := strings.ToLower(strings.TrimSpace(f.Name))
		if key == "" {
			continue
		}
		if strings.TrimSpace(f.Path) != "" && strings.TrimSpace(f.Value) != "" {
			return nil, fmt.Errorf("field %q: set path or value, not both", f.Name)
		}
		if strings.TrimSpace(f.Path) != "" {
			p, err := expandPath(f.Path)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", f.Name, err)
			}
			out[key] = p
			continue
		}
		out[key] = f.Value
	}
	return out, nil
}

func parseBoolField(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err == nil {
		return b
	}
	switch v {
	case "yes", "y", "on":
		return true
	default:
		return false
	}
}

func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[2:])
	}
	return filepath.Abs(p)
}

// PatternNameFromDir reads global.pattern from values-global.yaml when present.
func PatternNameFromDir(patternDir string) string {
	data, err := os.ReadFile(filepath.Join(patternDir, "values-global.yaml"))
	if err != nil {
		return ""
	}
	var doc struct {
		Global struct {
			Pattern string `yaml:"pattern"`
		} `yaml:"global"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ""
	}
	return doc.Global.Pattern
}
