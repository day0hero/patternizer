package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/validatedpatterns/patternizer/internal/embedded"
	"github.com/validatedpatterns/patternizer/internal/fileutils"
	"github.com/validatedpatterns/patternizer/internal/pattern"
)

// runUpgrade handles the upgrade logic for the upgrade command.
func runUpgrade(replaceMakefile bool) error {
	_, repoRoot, err := pattern.GetPatternNameAndRepoRoot()
	if err != nil {
		return fmt.Errorf("error getting pattern information: %w", err)
	}

	commonDirPath := filepath.Join(repoRoot, "common")
	patternShPath := filepath.Join(repoRoot, "pattern.sh")

	if err := fileutils.RemovePathIfExists(commonDirPath); err != nil {
		return fmt.Errorf("error removing common directory: %w", err)
	}

	if err := fileutils.RemovePathIfExists(patternShPath); err != nil {
		return fmt.Errorf("error removing pattern.sh: %w", err)
	}

	if err := fileutils.WriteEmbeddedFile(embedded.Resources, "resources/pattern.sh", patternShPath, 0o755); err != nil {
		return fmt.Errorf("error copying pattern.sh: %w", err)
	}

	if err := fileutils.WriteEmbeddedFile(embedded.Resources, "resources/Makefile-common", filepath.Join(repoRoot, "Makefile-common"), 0o644); err != nil {
		return fmt.Errorf("error copying Makefile-common: %w", err)
	}

	if err := fileutils.WriteEmbeddedFile(embedded.Resources, "resources/ansible.cfg", filepath.Join(repoRoot, "ansible.cfg"), 0o644); err != nil {
		return fmt.Errorf("error copying ansible.cfg: %w", err)
	}

	makefileDst := filepath.Join(repoRoot, "Makefile")

	if replaceMakefile {
		if err := fileutils.WriteEmbeddedFile(embedded.Resources, "resources/Makefile", makefileDst, 0o644); err != nil {
			return fmt.Errorf("error replacing Makefile: %w", err)
		}
	} else {
		if _, err := os.Stat(makefileDst); os.IsNotExist(err) {
			if err := fileutils.WriteEmbeddedFile(embedded.Resources, "resources/Makefile", makefileDst, 0o644); err != nil {
				return fmt.Errorf("error copying Makefile: %w", err)
			}
		} else if err == nil {
			hasInclude, err := fileutils.FileContainsIncludeMakefileCommon(makefileDst)
			if err != nil {
				return fmt.Errorf("error checking Makefile for include: %w", err)
			}
			if !hasInclude {
				if err := fileutils.PrependLineToFile(makefileDst, "include Makefile-common"); err != nil {
					return fmt.Errorf("error updating Makefile: %w", err)
				}
			}
		} else {
			return fmt.Errorf("error accessing Makefile: %w", err)
		}
	}

	if err := fileutils.InstallSkills(repoRoot); err != nil {
		return fmt.Errorf("error installing skills: %w", err)
	}

	warnings, err := collectUpgradeWarnings(repoRoot)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully upgraded pattern repository in %s\n", repoRoot)

	emitUpgradeWarnings(warnings)
	return nil
}

// collectUpgradeWarnings inspects the pattern's values-global.yaml and returns a list of
// compact deprecation warnings that should be surfaced to the user as a call to action.
func collectUpgradeWarnings(repoRoot string) ([]string, error) {
	var warnings []string

	deprecated, err := pattern.UsesDeprecatedClusterGroupName(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("error checking for deprecated fields: %w", err)
	}
	if deprecated {
		warnings = append(warnings, "WARNING: values-global.yaml uses deprecated 'main.clusterGroupName' (operator 0.0.78). Set 'main.variant' instead.")
	}

	variant, folderMissing, err := pattern.VariantFolderFileMissing(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("error checking for variants folder structure: %w", err)
	}
	if folderMissing {
		warnings = append(warnings, fmt.Sprintf("WARNING: not using the variants folder structure (variants/%s/values-%s.yaml not found; operator 0.0.78). See https://validatedpatterns.io/blog/2026-08-04-variants-folder-structure/", variant, variant))
	}

	missingSingleArgoCD, err := pattern.MissingSingleArgoCD(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("error checking for singleArgoCD: %w", err)
	}
	if missingSingleArgoCD {
		warnings = append(warnings, "WARNING: 'global.singleArgoCD' is not set. Set 'global.singleArgoCD: true' if possible. See https://validatedpatterns.io/blog/2026-06-04-single-argocd-option/")
	}

	return warnings, nil
}

// emitUpgradeWarnings prints deprecation warnings to stderr as a grouped block, separated
// from the normal stdout logs so they stand out as a call to action.
func emitUpgradeWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}

	fmt.Fprintln(os.Stderr)
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
}
