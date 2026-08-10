package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

func newAirgapCommand() *cobra.Command {
	opts := airgap.DefaultOptions()
	var clusterGroups string

	airgapCmd := &cobra.Command{
		Use:   "airgap",
		Short: "Disconnected / airgap helpers for Validated Patterns",
		Long:  "Generate oc-mirror ImageSetConfiguration and disconnected install artifacts from pattern values files.",
	}

	initCmd := &cobra.Command{
		Use:   "init <absolute-path|git-url>",
		Short: "Generate imagesetconfig.yaml and disconnected install artifacts",
		Long: `Analyze a Validated Pattern repository and generate:

  - imagesetconfig.yaml (oc-mirror v2)
  - values-disconnected.yaml
  - mirror-commands.sh
  - CHECKLIST.md

Pattern source must be an absolute path or a Git URL.
Git clones land in --output-dir (or --clone-dir if set).
Local helm chart paths in imagesetconfig.yaml are absolute
($OUTPUTDIR/<repo>/<chart/path> when cloned into the output directory).

With --bundle, also run:
  oc-mirror --config=$OUTPUTDIR/imagesetconfig.yaml file://$OUTPUTDIR --v2 \
    --registries.d=$OUTPUTDIR/registries.d

(--registries.d disables sigstore attachments for registry.connect.redhat.com)

Examples:
  patternizer airgap init /home/user/patterns/multicloud-gitops \
    --ocp-version 4.19 --registry registry.internal.disconnected.net

  patternizer airgap init https://github.com/validatedpatterns/multicloud-gitops \
    --git-ref main --ocp-version 4.19 --download-oc-mirror --bundle
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.PatternSource = args[0]
			if clusterGroups != "" {
				for _, g := range strings.Split(clusterGroups, ",") {
					g = strings.TrimSpace(g)
					if g != "" {
						opts.ClusterGroups = append(opts.ClusterGroups, g)
					}
				}
			}
			if opts.OutputDir != "" && !filepath.IsAbs(opts.OutputDir) {
				abs, err := filepath.Abs(opts.OutputDir)
				if err != nil {
					return err
				}
				opts.OutputDir = abs
			}
			inv, err := airgap.Init(&opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Airgap init complete for pattern %q\n", inv.PatternName)
			fmt.Fprintf(cmd.OutOrStdout(), "  source:         %s\n", opts.PatternSource)
			fmt.Fprintf(cmd.OutOrStdout(), "  pattern dir:    %s\n", inv.ResolvedDir)
			fmt.Fprintf(cmd.OutOrStdout(), "  operators:      %d\n", len(inv.Operators))
			fmt.Fprintf(cmd.OutOrStdout(), "  remote charts:  %d\n", len(inv.RemoteCharts))
			fmt.Fprintf(cmd.OutOrStdout(), "  local charts:   %d\n", len(inv.LocalCharts))
			fmt.Fprintf(cmd.OutOrStdout(), "  extra images:   %d\n", len(inv.AdditionalImages))
			fmt.Fprintf(cmd.OutOrStdout(), "  output:         %s\n", inv.OutputDir)
			if opts.Bundle {
				fmt.Fprintf(cmd.OutOrStdout(), "  bundle:         oc-mirror completed into %s\n", inv.OutputDir)
			}
			if len(inv.Warnings) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  warnings:       %d (see CHECKLIST.md)\n", len(inv.Warnings))
			}
			return nil
		},
	}

	initCmd.Flags().StringVar(&opts.OCPVersion, "ocp-version", opts.OCPVersion, "OpenShift minor version for catalog tags (e.g. 4.19)")
	initCmd.Flags().StringVar(&opts.Registry, "registry", opts.Registry, "Disconnected mirror registry hostname")
	initCmd.Flags().StringVar(&opts.OutputDir, "output-dir", opts.OutputDir, "Directory for generated artifacts (and Git clones by default)")
	initCmd.Flags().StringVar(&opts.HelmRepo, "helm-repo", opts.HelmRepo, "OCI registry/org for Validated Patterns charts")
	initCmd.Flags().BoolVar(&opts.IncludePlatform, "platform", false, "Include OCP platform release channel in ImageSetConfig")
	initCmd.Flags().BoolVar(&opts.WriteValues, "write-values", true, "Emit values-disconnected.yaml overlay")
	initCmd.Flags().BoolVar(&opts.SkipOCIResolve, "skip-oci-resolve", false, "Skip live OCI chart version resolution and image discovery (wildcards will not be resolved)")
	initCmd.Flags().StringVar(&clusterGroups, "cluster-groups", "", "Comma-separated cluster group names (default: all)")
	initCmd.Flags().StringVar(&opts.GitRef, "git-ref", "", "Branch or tag to clone when the source is a Git URL")
	initCmd.Flags().StringVar(&opts.CloneDir, "clone-dir", "", "Directory for Git clones (default: --output-dir)")
	initCmd.Flags().BoolVar(&opts.DownloadOCMirror, "download-oc-mirror", false, "Download oc-mirror.tar.gz and oc-mirror.rhel9.tar.gz into --output-dir")
	initCmd.Flags().BoolVar(&opts.Bundle, "bundle", false, "After generating artifacts, run oc-mirror into --output-dir")

	var mirrorLogPath string
	parseLogCmd := &cobra.Command{
		Use:   "parse-mirror-log [logfile]",
		Short: "Extract skipped oci:// related images from an oc-mirror log",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := mirrorLogPath
			if len(args) == 1 {
				path = args[0]
			}
			if path == "" {
				return fmt.Errorf("provide a log file path")
			}
			images, err := airgap.ParseMirrorLog(path)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), airgap.FormatSkippedImagesYAML(images))
			if len(images) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Found %d skipped oci:// image(s)\n", len(images))
			}
			return nil
		},
	}
	parseLogCmd.Flags().StringVar(&mirrorLogPath, "log", "", "Path to oc-mirror log file")

	airgapCmd.AddCommand(initCmd)
	airgapCmd.AddCommand(parseLogCmd)
	return airgapCmd
}
