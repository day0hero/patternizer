package airgap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/validatedpatterns/patternizer/internal/types"
)

// PatternInventory holds discovered pattern content for airgap init.
type PatternInventory struct {
	PatternName          string
	ClusterGroupName     string
	Variant              string
	ClusterGroupChartVer string
	HelmRepoURL          string
	ResolvedDir          string
	OutputDir            string
	Groups               []ClusterGroupInventory
	Operators            []OperatorRef
	RemoteCharts         []RemoteChart
	LocalCharts          []LocalChart
	AdditionalImages     []string
	ExtraOperators       []OperatorCatalog
	ExtraConfig          *ExtraConfig
	ManagedClusterGroups []string
	Warnings             []string
}

// ClusterGroupInventory is one values-<group>.yaml parse result.
type ClusterGroupInventory struct {
	Name            string
	File            string
	Subscriptions   []OperatorRef
	Applications    []types.Application
	ImperativeImage string
}

// RemoteChart is an OCI Helm chart referenced by chart/chartVersion.
type RemoteChart struct {
	Name              string
	VersionConstraint string
	ResolvedVersion   string
	PullSpec          string
	Images            []string
}

// LocalChart is a Helm chart path for oc-mirror helm.local (from --extra-config).
type LocalChart struct {
	Name   string
	Path   string
	Images []string
}

// LoadPattern discovers and parses values files for the pattern.
func LoadPattern(opts *Options) (*PatternInventory, error) {
	root := opts.PatternDir
	globalPath := filepath.Join(root, "values-global.yaml")
	data, err := os.ReadFile(globalPath)
	if err != nil {
		return nil, fmt.Errorf("read values-global.yaml: %w", err)
	}

	var vg types.ValuesGlobal
	if err := yaml.Unmarshal(data, &vg); err != nil {
		return nil, fmt.Errorf("parse values-global.yaml: %w", err)
	}

	variant := vg.Main.Variant
	if variant == "" {
		if legacy, ok := vg.Main.OtherFields["clusterGroupName"].(string); ok {
			variant = legacy
		}
	}

	inv := &PatternInventory{
		PatternName:          vg.Global.Pattern,
		ClusterGroupName:     variant,
		Variant:              variant,
		ClusterGroupChartVer: vg.Main.MultiSourceConfig.ClusterGroupChartVersion,
		HelmRepoURL:          opts.HelmRepo,
	}
	if inv.ClusterGroupChartVer == "" {
		inv.ClusterGroupChartVer = "0.9.*"
	}

	groupFiles, err := discoverClusterGroupFiles(root, variant, opts)
	if err != nil {
		return nil, err
	}
	if len(groupFiles) == 0 {
		return nil, fmt.Errorf("no cluster group values files found under %s (checked classic values-*.yaml and variants/%s/)", root, variant)
	}

	loaded := map[string]bool{}
	spokeDirs := allValuesSearchDirs(root, variant)
	for _, gf := range groupFiles {
		cgi, err := loadClusterGroupFile(gf)
		if err != nil {
			inv.Warnings = append(inv.Warnings, fmt.Sprintf("skip %s: %v", gf, err))
			continue
		}
		inv.Groups = append(inv.Groups, cgi)
		loaded[cgi.Name] = true
		for _, mc := range extractManagedClusterGroups(gf) {
			inv.ManagedClusterGroups = appendUnique(inv.ManagedClusterGroups, mc)
		}
	}

	for _, mc := range inv.ManagedClusterGroups {
		if loaded[mc] {
			continue
		}
		spokePath := findValuesFile(spokeDirs, "values-"+mc+".yaml")
		if spokePath == "" {
			inv.Warnings = append(inv.Warnings,
				fmt.Sprintf("managedClusterGroup %q referenced but values-%s.yaml not found in %v", mc, mc, spokeDirs))
			continue
		}
		cgi, err := loadClusterGroupFile(spokePath)
		if err != nil {
			inv.Warnings = append(inv.Warnings, fmt.Sprintf("skip spoke %s: %v", spokePath, err))
			continue
		}
		inv.Groups = append(inv.Groups, cgi)
		loaded[cgi.Name] = true
		inv.Warnings = append(inv.Warnings,
			fmt.Sprintf("auto-included spoke cluster group %q from managedClusterGroups (%s)", mc, spokePath))
	}

	return inv, nil
}

func primaryValuesSearchDirs(root, variant string) []string {
	var dirs []string
	if variant != "" {
		vdir := filepath.Join(root, "variants", variant)
		if info, err := os.Stat(vdir); err == nil && info.IsDir() {
			dirs = append(dirs, vdir)
		}
	}
	dirs = append(dirs, root)
	return dirs
}

func allValuesSearchDirs(root, variant string) []string {
	dirs := primaryValuesSearchDirs(root, variant)
	variantsRoot := filepath.Join(root, "variants")
	entries, err := os.ReadDir(variantsRoot)
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(variantsRoot, e.Name())
		already := false
		for _, existing := range dirs {
			if existing == d {
				already = true
				break
			}
		}
		if !already {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func findValuesFile(dirs []string, name string) string {
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func discoverClusterGroupFiles(root, variant string, opts *Options) ([]string, error) {
	wanted := map[string]bool{}
	for _, g := range opts.ClusterGroups {
		wanted[g] = true
	}

	dirs := primaryValuesSearchDirs(root, variant)
	seenGroups := map[string]string{}
	var order []string

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
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
			if base == "standalone" && dir == root {
				continue
			}
			if strings.HasSuffix(base, ".template") || strings.Contains(base, "secret") {
				continue
			}
			if isVersionOverlay(base) {
				continue
			}
			groupName := base
			if len(wanted) > 0 && !wanted[groupName] {
				continue
			}
			if _, exists := seenGroups[groupName]; exists {
				continue
			}
			seenGroups[groupName] = filepath.Join(dir, name)
			order = append(order, groupName)
		}
	}

	files := make([]string, 0, len(order))
	for _, g := range order {
		files = append(files, seenGroups[g])
	}
	sort.Strings(files)
	return files, nil
}

func isVersionOverlay(base string) bool {
	parts := strings.SplitN(base, "-", 2)
	if len(parts) < 2 {
		return false
	}
	return parts[0] != "" && parts[0][0] >= '0' && parts[0][0] <= '9'
}

func loadClusterGroupFile(path string) (ClusterGroupInventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ClusterGroupInventory{}, err
	}
	var vcg types.ValuesClusterGroup
	if err := yaml.Unmarshal(data, &vcg); err != nil {
		return ClusterGroupInventory{}, err
	}

	name := vcg.ClusterGroup.Name
	if name == "" {
		base := filepath.Base(path)
		name = strings.TrimSuffix(strings.TrimPrefix(base, "values-"), ".yaml")
	}

	cgi := ClusterGroupInventory{
		Name: name,
		File: path,
	}

	for _, sub := range vcg.ClusterGroup.Subscriptions {
		if sub.Name == "" {
			continue
		}
		cgi.Subscriptions = append(cgi.Subscriptions, OperatorRef{
			Name:    sub.Name,
			Channel: sub.Channel,
			Catalog: LookupCatalog(sub.Name),
			Source:  sub.Source,
		})
	}

	for _, app := range vcg.ClusterGroup.Applications {
		if app.Name == "" && app.Chart == "" && app.Path == "" {
			continue
		}
		cgi.Applications = append(cgi.Applications, app)
	}

	cgi.ImperativeImage = extractImperativeImage(data)
	return cgi, nil
}

func extractImperativeImage(raw []byte) string {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	cg, _ := doc["clusterGroup"].(map[string]interface{})
	if cg == nil {
		return ""
	}
	imp, _ := cg["imperative"].(map[string]interface{})
	if imp == nil {
		return ""
	}
	if img, ok := imp["image"].(string); ok {
		return img
	}
	return ""
}

func extractManagedClusterGroups(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	cg, _ := doc["clusterGroup"].(map[string]interface{})
	if cg == nil {
		return nil
	}
	mcg, _ := cg["managedClusterGroups"].(map[string]interface{})
	if mcg == nil {
		return nil
	}
	var names []string
	for _, v := range mcg {
		entry, _ := v.(map[string]interface{})
		if entry == nil {
			continue
		}
		if n, ok := entry["name"].(string); ok && n != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

func appendUnique(slice []string, v string) []string {
	for _, s := range slice {
		if s == v {
			return slice
		}
	}
	return append(slice, v)
}
