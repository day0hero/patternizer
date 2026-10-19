package airgap

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DisconnectedValuesConfig drives rendering of pattern values for airgap.
type DisconnectedValuesConfig struct {
	HelmRepoURL string
	RedHatCS    string
	CommunityCS string
	Marketplace string
}

// ResolveDisconnectedValues builds helm repo + CatalogSource names from airgap spec / flags / oc.
func ResolveDisconnectedValues(spec *AirgapSpec, opts *BootstrapOptions) (*DisconnectedValuesConfig, error) {
	if spec == nil {
		spec = &AirgapSpec{}
	}

	registry := strings.TrimSpace(opts.Registry)
	if registry == "" {
		registry = strings.TrimSpace(spec.Registry)
	}
	helmURL := strings.TrimSpace(spec.HelmRepoURL)
	if helmURL == "" && registry != "" {
		helmURL = strings.TrimSuffix(registry, "/") + "/validatedpatterns"
	}
	// Also accept registry from first registry url host if still empty.
	if helmURL == "" && len(spec.Registries) > 0 {
		fields, _ := resolveFieldMap(spec.Registries[0].Fields)
		if u := NormalizeRegistryURL(fields["url"]); u != "" {
			host := u
			if i := strings.Index(u, "/"); i >= 0 {
				host = u[:i]
			}
			helmURL = host + "/validatedpatterns"
		}
	}

	ocp := strings.TrimSpace(opts.OCPVersion)
	if ocp == "" {
		ocp = strings.TrimSpace(spec.OCPVersion)
	}

	cs := map[string]string{}
	for k, v := range spec.CatalogSources {
		cs[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}

	discover := opts.DiscoverCatalogs || spec.DiscoverCatalogs
	if discover {
		found, err := DiscoverCatalogSources()
		if err != nil {
			return nil, fmt.Errorf("discover CatalogSources: %w", err)
		}
		for k, v := range found {
			if cs[k] == "" {
				cs[k] = v
			}
		}
	}

	if cs["redhat"] == "" && ocp != "" {
		cs["redhat"] = CatalogSourceName(CatalogRedHat, ocp)
	}
	if cs["community"] == "" && ocp != "" {
		cs["community"] = CatalogSourceName(CatalogCommunity, ocp)
	}
	if cs["marketplace"] == "" && ocp != "" {
		cs["marketplace"] = CatalogSourceName(CatalogMarketplace, ocp)
	}

	cfg := &DisconnectedValuesConfig{
		HelmRepoURL: helmURL,
		RedHatCS:    cs["redhat"],
		CommunityCS: cs["community"],
		Marketplace: cs["marketplace"],
	}
	if cfg.HelmRepoURL == "" && cfg.RedHatCS == "" && cfg.CommunityCS == "" {
		return nil, fmt.Errorf("cannot render values: set airgap.registry (or helmRepoUrl), and airgap.ocpVersion or airgap.catalogSources (or --discover-catalogs)")
	}
	return cfg, nil
}

// DiscoverCatalogSources runs oc to find redhat/community/marketplace CatalogSource names.
func DiscoverCatalogSources() (map[string]string, error) {
	if _, err := exec.LookPath("oc"); err != nil {
		return nil, fmt.Errorf("oc not found on PATH")
	}
	cmd := exec.Command("oc", "get", "catalogsource", "-A", "-o", "json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("oc get catalogsource: %w (%s)", err, truncate(string(out), 300))
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse catalogsource JSON: %w", err)
	}

	outMap := map[string]string{}
	pick := func(kind, prefix string) {
		if outMap[kind] != "" {
			return
		}
		best := ""
		for _, it := range list.Items {
			name := it.Metadata.Name
			if strings.HasPrefix(name, prefix) {
				if best == "" || name > best {
					best = name
				}
			}
		}
		if best != "" {
			outMap[kind] = best
		}
	}
	pick("redhat", "cs-redhat-operator-index-")
	pick("community", "cs-community-operator-index-")
	pick("marketplace", "cs-redhat-marketplace-index-")
	if len(outMap) == 0 {
		return nil, fmt.Errorf("no oc-mirror style CatalogSources found (expected cs-redhat-operator-index-* / cs-community-operator-index-*)")
	}
	return outMap, nil
}

// RenderPatternValues merges disconnected settings into values-global.yaml and cluster-group values files.
// Originals are backed up under outDir/backup/values/.
func RenderPatternValues(patternDir, outDir string, cfg *DisconnectedValuesConfig) ([]string, error) {
	if patternDir == "" {
		return nil, fmt.Errorf("pattern dir is required to render values")
	}
	if cfg == nil {
		return nil, fmt.Errorf("disconnected values config is required")
	}

	backupRoot := filepath.Join(outDir, "backup", "values")
	renderedRoot := filepath.Join(outDir, "rendered")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(renderedRoot, 0o755); err != nil {
		return nil, err
	}

	var written []string

	globalPath := filepath.Join(patternDir, "values-global.yaml")
	if _, err := os.Stat(globalPath); err == nil {
		if err := renderOneValuesFile(globalPath, backupRoot, renderedRoot, patternDir, cfg, true); err != nil {
			return written, err
		}
		written = append(written, globalPath)
	}

	files, err := listClusterGroupValuesFiles(patternDir)
	if err != nil {
		return written, err
	}
	for _, f := range files {
		if err := renderOneValuesFile(f, backupRoot, renderedRoot, patternDir, cfg, false); err != nil {
			return written, fmt.Errorf("%s: %w", f, err)
		}
		written = append(written, f)
	}
	return written, nil
}

func listClusterGroupValuesFiles(patternDir string) ([]string, error) {
	var files []string
	seen := map[string]bool{}

	add := func(path string) {
		if seen[path] {
			return
		}
		seen[path] = true
		files = append(files, path)
	}

	// Classic layout at repo root.
	entries, err := os.ReadDir(patternDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "values-") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		base := strings.TrimSuffix(strings.TrimPrefix(name, "values-"), ".yaml")
		switch base {
		case "global", "secret", "devel":
			continue
		}
		if strings.Contains(base, "secret") || strings.HasSuffix(base, ".template") {
			continue
		}
		add(filepath.Join(patternDir, name))
	}

	// variants/<variant>/values-*.yaml
	variantsRoot := filepath.Join(patternDir, "variants")
	vEntries, err := os.ReadDir(variantsRoot)
	if err == nil {
		for _, ve := range vEntries {
			if !ve.IsDir() {
				continue
			}
			vdir := filepath.Join(variantsRoot, ve.Name())
			filesIn, err := os.ReadDir(vdir)
			if err != nil {
				continue
			}
			for _, e := range filesIn {
				if e.IsDir() {
					continue
				}
				name := e.Name()
				if !strings.HasPrefix(name, "values-") || !strings.HasSuffix(name, ".yaml") {
					continue
				}
				base := strings.TrimSuffix(strings.TrimPrefix(name, "values-"), ".yaml")
				if base == "global" || strings.Contains(base, "secret") {
					continue
				}
				add(filepath.Join(vdir, name))
			}
		}
	}

	sort.Strings(files)
	return files, nil
}

func renderOneValuesFile(path, backupRoot, renderedRoot, patternDir string, cfg *DisconnectedValuesConfig, isGlobal bool) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	rel, err := filepath.Rel(patternDir, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	bakPath := filepath.Join(backupRoot, rel)
	if err := os.MkdirAll(filepath.Dir(bakPath), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(bakPath); err != nil {
		// First backup only.
		if err := os.WriteFile(bakPath, raw, 0o644); err != nil {
			return err
		}
	}

	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}

	if isGlobal {
		applyGlobalDisconnected(doc, cfg)
	} else {
		applyClusterGroupDisconnected(doc, cfg)
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	header := "# Updated by patternizer airgap bootstrap (disconnected CatalogSources / helmRepoUrl)\n"
	content := append([]byte(header), out...)

	rendPath := filepath.Join(renderedRoot, rel)
	if err := os.MkdirAll(filepath.Dir(rendPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(rendPath, content, 0o644); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func applyGlobalDisconnected(doc map[string]interface{}, cfg *DisconnectedValuesConfig) {
	main := mapFrom(doc, "main")
	msc := mapFrom(main, "multiSourceConfig")
	msc["enabled"] = true
	if cfg.HelmRepoURL != "" {
		msc["helmRepoUrl"] = cfg.HelmRepoURL
	}
	if cfg.CommunityCS != "" {
		po := mapFrom(main, "patternsOperator")
		po["source"] = cfg.CommunityCS
	}
	if cfg.RedHatCS != "" {
		gitops := mapFrom(main, "gitops")
		gitops["operatorSource"] = cfg.RedHatCS
	}
}

func applyClusterGroupDisconnected(doc map[string]interface{}, cfg *DisconnectedValuesConfig) {
	if cfg.RedHatCS != "" {
		acm := mapFrom(doc, "acm")
		mce := mapFrom(acm, "mce_operator")
		mce["source"] = cfg.RedHatCS
	}

	cg := mapFrom(doc, "clusterGroup")
	subs, _ := cg["subscriptions"].(map[string]interface{})
	if subs == nil {
		return
	}
	for _, v := range subs {
		entry, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		kind := LookupCatalog(name)
		var src string
		switch kind {
		case CatalogCommunity:
			src = cfg.CommunityCS
		case CatalogMarketplace:
			src = cfg.Marketplace
			if src == "" {
				src = cfg.RedHatCS
			}
		default:
			src = cfg.RedHatCS
		}
		if src != "" {
			entry["source"] = src
		}
	}
}

func mapFrom(parent map[string]interface{}, key string) map[string]interface{} {
	if parent == nil {
		return map[string]interface{}{}
	}
	if existing, ok := parent[key].(map[string]interface{}); ok && existing != nil {
		return existing
	}
	m := map[string]interface{}{}
	parent[key] = m
	return m
}
