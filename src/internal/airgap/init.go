package airgap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Init generates airgap artifacts for a pattern repository.
func Init(opts *Options) (*PatternInventory, error) {
	if opts == nil {
		return nil, fmt.Errorf("options are required")
	}
	sourceInput := opts.PatternSource
	if sourceInput == "" {
		sourceInput = opts.PatternDir
	}

	if opts.OutputDir == "" {
		opts.OutputDir = "./airgap-out"
	}
	outAbs, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return nil, err
	}
	opts.OutputDir = outAbs

	if opts.CloneDir == "" && IsGitURL(sourceInput) {
		opts.CloneDir = opts.OutputDir
	}

	src, err := ResolvePatternSource(sourceInput, opts)
	if err != nil {
		return nil, err
	}
	opts.PatternDir = src.Dir

	if opts.OCPVersion == "" {
		opts.OCPVersion = "4.19"
	}
	if opts.Registry == "" {
		opts.Registry = "registry.internal.disconnected.net"
	}
	if opts.HelmRepo == "" {
		opts.HelmRepo = "quay.io/validatedpatterns"
	}

	inv, err := LoadPattern(opts)
	if err != nil {
		return nil, err
	}
	inv.ResolvedDir = src.Dir
	inv.OutputDir = opts.OutputDir
	inv.Warnings = append(inv.Warnings, opts.ExtraWarnings...)
	if src.FromGit {
		inv.Warnings = append(inv.Warnings,
			fmt.Sprintf("pattern cloned from %s into %s", src.GitURL, src.Dir))
	}

	if err := enrichInventory(inv, opts); err != nil {
		return inv, err
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return inv, err
	}

	if opts.DownloadOCMirror {
		if err := DownloadOCMirrorTools(opts.OCPVersion, opts.OutputDir); err != nil {
			return inv, fmt.Errorf("download oc-mirror: %w", err)
		}
	}

	isc := BuildImageSet(inv, opts)
	if err := WriteImageSet(isc, opts.OutputDir); err != nil {
		return inv, fmt.Errorf("write imagesetconfig: %w", err)
	}

	if opts.WriteValues {
		overlay := BuildValuesOverlay(inv, opts)
		if err := WriteValuesOverlay(overlay, opts.OutputDir); err != nil {
			return inv, fmt.Errorf("write values overlay: %w", err)
		}
	}

	if err := WriteMirrorCommands(opts, opts.OutputDir); err != nil {
		return inv, fmt.Errorf("write mirror commands: %w", err)
	}
	if err := WriteChecklist(inv, opts, opts.OutputDir); err != nil {
		return inv, fmt.Errorf("write checklist: %w", err)
	}

	if opts.Bundle {
		cfg := filepath.Join(opts.OutputDir, "imagesetconfig.yaml")
		if err := RunOCMirror(cfg, opts.OutputDir); err != nil {
			return inv, fmt.Errorf("bundle (oc-mirror): %w", err)
		}
	}

	return inv, nil
}

func enrichInventory(inv *PatternInventory, opts *Options) error {
	opIndex := map[string]*OperatorRef{}

	addOp := func(op OperatorRef) {
		key := op.Name
		if existing, ok := opIndex[key]; ok {
			if existing.Channel == "" && op.Channel != "" {
				existing.Channel = op.Channel
			}
			if existing.Catalog == "" {
				existing.Catalog = op.Catalog
			}
			return
		}
		cp := op
		opIndex[key] = &cp
	}

	for _, op := range FrameworkOperators() {
		addOp(op)
	}

	hasACM := false
	for _, g := range inv.Groups {
		for _, op := range g.Subscriptions {
			addOp(op)
			if op.Name == "advanced-cluster-management" {
				hasACM = true
			}
		}
		if g.ImperativeImage != "" {
			inv.AdditionalImages = appendUnique(inv.AdditionalImages, g.ImperativeImage)
		}
	}
	if hasACM {
		for _, op := range ACMCompanionOperators() {
			addOp(op)
		}
	}

	cgConstraint := inv.ClusterGroupChartVer
	inv.RemoteCharts = append(inv.RemoteCharts, resolveRemoteChart(inv, opts, "clustergroup", cgConstraint))

	seenCharts := map[string]bool{"clustergroup": true}
	seenLocal := map[string]bool{}

	for _, g := range inv.Groups {
		for _, app := range g.Applications {
			if app.Chart != "" {
				if seenCharts[app.Chart] {
					continue
				}
				seenCharts[app.Chart] = true
				constraint := app.ChartVersion
				if constraint == "" {
					constraint = "*"
				}
				inv.RemoteCharts = append(inv.RemoteCharts, resolveRemoteChart(inv, opts, app.Chart, constraint))
				continue
			}
			if app.Path != "" {
				if seenLocal[app.Path] {
					continue
				}
				seenLocal[app.Path] = true
				name := app.Name
				if name == "" {
					name = filepath.Base(app.Path)
				}
				imgs, warnings, err := DiscoverImages(opts.PatternDir, app.Path)
				if err != nil {
					inv.Warnings = append(inv.Warnings, fmt.Sprintf("local chart %s: %v", app.Path, err))
				}
				inv.Warnings = append(inv.Warnings, warnings...)
				absPath := app.Path
				if !filepath.IsAbs(absPath) {
					absPath = filepath.Join(opts.PatternDir, app.Path)
				}
				inv.LocalCharts = append(inv.LocalCharts, LocalChart{
					Name:   name,
					Path:   absPath,
					Images: imgs,
				})
			}
		}
	}

	for _, img := range FrameworkImages(opts.HelmRepo) {
		inv.AdditionalImages = appendUnique(inv.AdditionalImages, img)
	}

	keys := make([]string, 0, len(opIndex))
	for k := range opIndex {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		inv.Operators = append(inv.Operators, *opIndex[k])
	}

	sort.Slice(inv.RemoteCharts, func(i, j int) bool {
		return inv.RemoteCharts[i].Name < inv.RemoteCharts[j].Name
	})
	sort.Slice(inv.LocalCharts, func(i, j int) bool {
		return inv.LocalCharts[i].Path < inv.LocalCharts[j].Path
	})
	sort.Strings(inv.AdditionalImages)

	return nil
}

func resolveRemoteChart(inv *PatternInventory, opts *Options, chartName, constraint string) RemoteChart {
	rc := RemoteChart{
		Name:              chartName,
		VersionConstraint: constraint,
	}
	ver, err := ResolveOCIChartVersion(opts.HelmRepo, chartName, constraint, opts.SkipOCIResolve)
	if err != nil {
		inv.Warnings = append(inv.Warnings, fmt.Sprintf(
			"chart %s: failed to resolve %q (%v); not adding to additionalImages",
			chartName, constraint, err))
		return rc
	}
	rc.ResolvedVersion = ver
	rc.PullSpec = fmt.Sprintf("%s/%s:%s", opts.HelmRepo, chartName, ver)

	imgs, warns, discErr := DiscoverOCIChartImages(opts.HelmRepo, chartName, ver, opts.SkipOCIResolve)
	inv.Warnings = append(inv.Warnings, warns...)
	if discErr != nil {
		inv.Warnings = append(inv.Warnings, fmt.Sprintf("chart %s image discovery: %v", chartName, discErr))
	}
	rc.Images = imgs
	for _, img := range imgs {
		inv.AdditionalImages = appendUnique(inv.AdditionalImages, img)
	}
	return rc
}
