package airgap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExtraConfig is an optional external file merged into the generated ImageSetConfiguration.
// Schema matches a subset of oc-mirror ImageSetConfiguration mirror fields:
//
//	operators:
//	  - catalog: registry.redhat.io/redhat/redhat-operator-index:v4.22
//	    packages:
//	      - name: lvms-operator
//	additionalImages:
//	  - name: registry.example.com/app:1.0
//	helm:
//	  local:
//	    - name: my-chart
//	      path: /abs/path/to/chart
type ExtraConfig struct {
	Operators        []OperatorCatalog `yaml:"operators,omitempty"`
	AdditionalImages []NamedImage      `yaml:"additionalImages,omitempty"`
	Helm             *HelmMirror       `yaml:"helm,omitempty"`
}

// LoadExtraConfig reads and validates an external extras YAML file.
// The file may use top-level operators/additionalImages/helm keys, or the same
// fields nested under mirror: (ImageSetConfiguration style).
func LoadExtraConfig(path string) (*ExtraConfig, error) {
	if path == "" {
		return nil, fmt.Errorf("extra config path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve extra config path: %w", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read extra config %s: %w", abs, err)
	}

	cfg, err := parseExtraConfigYAML(data)
	if err != nil {
		return nil, fmt.Errorf("parse extra config %s: %w", abs, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("extra config %s: %w", abs, err)
	}
	return cfg, nil
}

func parseExtraConfigYAML(data []byte) (*ExtraConfig, error) {
	var wrapped struct {
		Mirror      *ExtraConfig `yaml:"mirror"`
		ExtraConfig `yaml:",inline"`
	}
	if err := yaml.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Mirror != nil {
		return wrapped.Mirror, nil
	}
	cfg := wrapped.ExtraConfig
	return &cfg, nil
}

func (c *ExtraConfig) validate() error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	for i, op := range c.Operators {
		if strings.TrimSpace(op.Catalog) == "" {
			return fmt.Errorf("operators[%d]: catalog is required", i)
		}
		for j, pkg := range op.Packages {
			if strings.TrimSpace(pkg.Name) == "" {
				return fmt.Errorf("operators[%d].packages[%d]: name is required", i, j)
			}
		}
	}
	for i, img := range c.AdditionalImages {
		if strings.TrimSpace(img.Name) == "" {
			return fmt.Errorf("additionalImages[%d]: name is required", i)
		}
	}
	if c.Helm != nil {
		for i, lc := range c.Helm.Local {
			name := strings.TrimSpace(lc.Name)
			path := strings.TrimSpace(lc.Path)
			if name == "" && path == "" {
				continue
			}
			if path == "" {
				return fmt.Errorf("helm.local[%d]: path is required when name is set", i)
			}
		}
	}
	return nil
}

// ApplyExtraConfig merges operators, additional images, and helm.local charts into the inventory.
// BuildImageSet then writes those fields into imagesetconfig.yaml under mirror: so oc-mirror
// can pull them.
func ApplyExtraConfig(inv *PatternInventory, cfg *ExtraConfig, opts *Options) error {
	if inv == nil || cfg == nil {
		return nil
	}

	inv.ExtraConfig = cfg
	inv.ExtraOperators = append(inv.ExtraOperators, cfg.Operators...)

	for _, img := range cfg.AdditionalImages {
		name := strings.TrimSpace(img.Name)
		if name == "" {
			continue
		}
		inv.AdditionalImages = appendUnique(inv.AdditionalImages, name)
	}

	if cfg.Helm == nil {
		return nil
	}

	seen := map[string]bool{}
	for _, existing := range inv.LocalCharts {
		seen[existing.Path] = true
	}

	resolvedLocal := make([]HelmLocalChart, 0, len(cfg.Helm.Local))
	for _, lc := range cfg.Helm.Local {
		name := strings.TrimSpace(lc.Name)
		path := strings.TrimSpace(lc.Path)
		if name == "" && path == "" {
			continue
		}
		absPath := path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(filepath.Dir(opts.ExtraConfigPath), path)
			var err error
			absPath, err = filepath.Abs(absPath)
			if err != nil {
				return fmt.Errorf("resolve helm.local path %q: %w", path, err)
			}
		}
		if name == "" {
			name = filepath.Base(absPath)
		}
		resolvedLocal = append(resolvedLocal, HelmLocalChart{Name: name, Path: absPath})
		if seen[absPath] {
			continue
		}
		seen[absPath] = true

		imgs, warnings, err := DiscoverImagesFromChartDir(absPath)
		if err != nil {
			inv.Warnings = append(inv.Warnings, fmt.Sprintf("extra-config local chart %s: %v", absPath, err))
		}
		inv.Warnings = append(inv.Warnings, warnings...)
		for _, img := range imgs {
			inv.AdditionalImages = appendUnique(inv.AdditionalImages, img)
		}
		inv.LocalCharts = append(inv.LocalCharts, LocalChart{
			Name:   name,
			Path:   absPath,
			Images: imgs,
		})
	}
	// Keep ExtraConfig paths absolute so the final ImageSet merge is idempotent.
	cfg.Helm.Local = resolvedLocal
	return nil
}

// MergeExtraConfigIntoImageSet merges operators, additionalImages, and helm.local from
// ExtraConfig directly into an ImageSetConfiguration (idempotent with inventory-based merge).
func MergeExtraConfigIntoImageSet(isc *ImageSetConfiguration, cfg *ExtraConfig) {
	if isc == nil || cfg == nil {
		return
	}

	isc.Mirror.Operators = MergeOperatorCatalogs(isc.Mirror.Operators, cfg.Operators)

	seenImg := map[string]bool{}
	for _, img := range isc.Mirror.AdditionalImages {
		seenImg[img.Name] = true
	}
	for _, img := range cfg.AdditionalImages {
		name := strings.TrimSpace(img.Name)
		if name == "" || seenImg[name] {
			continue
		}
		seenImg[name] = true
		isc.Mirror.AdditionalImages = append(isc.Mirror.AdditionalImages, NamedImage{Name: name})
	}

	if cfg.Helm == nil {
		return
	}
	if isc.Mirror.Helm == nil {
		isc.Mirror.Helm = &HelmMirror{}
	}
	seenPath := map[string]bool{}
	for _, lc := range isc.Mirror.Helm.Local {
		seenPath[lc.Path] = true
	}
	for _, lc := range cfg.Helm.Local {
		name := strings.TrimSpace(lc.Name)
		path := strings.TrimSpace(lc.Path)
		if name == "" && path == "" {
			continue
		}
		if path == "" || seenPath[path] {
			continue
		}
		seenPath[path] = true
		if name == "" {
			name = filepath.Base(path)
		}
		isc.Mirror.Helm.Local = append(isc.Mirror.Helm.Local, HelmLocalChart{
			Name: name,
			Path: path,
		})
	}
}

// MergeOperatorCatalogs appends extra catalogs into base, combining packages when catalog strings match.
func MergeOperatorCatalogs(base, extra []OperatorCatalog) []OperatorCatalog {
	if len(extra) == 0 {
		return base
	}
	out := make([]OperatorCatalog, len(base))
	copy(out, base)
	index := map[string]int{}
	for i, cat := range out {
		index[cat.Catalog] = i
	}
	for _, cat := range extra {
		catalog := strings.TrimSpace(cat.Catalog)
		if catalog == "" {
			continue
		}
		if i, ok := index[catalog]; ok {
			out[i].Packages = mergePackages(out[i].Packages, cat.Packages)
			continue
		}
		index[catalog] = len(out)
		out = append(out, OperatorCatalog{
			Catalog:  catalog,
			Packages: append([]OperatorPackage(nil), cat.Packages...),
		})
	}
	return out
}

func mergePackages(base, extra []OperatorPackage) []OperatorPackage {
	seen := map[string]int{}
	out := append([]OperatorPackage(nil), base...)
	for i, pkg := range out {
		seen[pkg.Name] = i
	}
	for _, pkg := range extra {
		name := strings.TrimSpace(pkg.Name)
		if name == "" {
			continue
		}
		if i, ok := seen[name]; ok {
			if out[i].DefaultChannel == "" && pkg.DefaultChannel != "" {
				out[i].DefaultChannel = pkg.DefaultChannel
			}
			out[i].Channels = mergeChannels(out[i].Channels, pkg.Channels)
			continue
		}
		seen[name] = len(out)
		out = append(out, pkg)
	}
	return out
}

func mergeChannels(base, extra []OperatorChannel) []OperatorChannel {
	seen := map[string]bool{}
	out := append([]OperatorChannel(nil), base...)
	for _, ch := range out {
		seen[ch.Name] = true
	}
	for _, ch := range extra {
		if ch.Name == "" || seen[ch.Name] {
			continue
		}
		seen[ch.Name] = true
		out = append(out, ch)
	}
	return out
}
