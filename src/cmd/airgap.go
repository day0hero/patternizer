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

Path-based (local) Helm charts referenced by applications are scanned for
container images only (added to additionalImages; charts themselves are not
mirrored). Provide extra operators, images, and helm.local charts via --extra-config.

Examples:
  patternizer airgap init /home/user/patterns/multicloud-gitops \
    --extra-config ~/extra-images.yaml

  patternizer airgap init /home/user/patterns/multicloud-gitops \
    --ocp-version 4.19 \
    --extra-config ~/extra-images.yaml

  patternizer airgap init https://github.com/validatedpatterns/multicloud-gitops \
    --git-ref main --download-oc-mirror
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
			if opts.ExtraConfigPath != "" && !filepath.IsAbs(opts.ExtraConfigPath) {
				abs, err := filepath.Abs(opts.ExtraConfigPath)
				if err != nil {
					return err
				}
				opts.ExtraConfigPath = abs
			}
			inv, err := airgap.Init(&opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Airgap init complete for pattern %q\n", inv.PatternName)
			fmt.Fprintf(cmd.OutOrStdout(), "  source:         %s\n", opts.PatternSource)
			fmt.Fprintf(cmd.OutOrStdout(), "  pattern dir:    %s\n", inv.ResolvedDir)
			extraOps := 0
			for _, cat := range inv.ExtraOperators {
				extraOps += len(cat.Packages)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  operators:      %d\n", len(inv.Operators)+extraOps)
			fmt.Fprintf(cmd.OutOrStdout(), "  remote charts:  %d\n", len(inv.RemoteCharts))
			fmt.Fprintf(cmd.OutOrStdout(), "  local charts:   %d\n", len(inv.LocalCharts))
			fmt.Fprintf(cmd.OutOrStdout(), "  extra images:   %d\n", len(inv.AdditionalImages))
			fmt.Fprintf(cmd.OutOrStdout(), "  output:         %s\n", inv.OutputDir)
			if opts.ExtraConfigPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  extra-config:   %s (merged into imagesetconfig.yaml)\n", opts.ExtraConfigPath)
			}
			if len(inv.Warnings) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  warnings:       %d (see CHECKLIST.md)\n", len(inv.Warnings))
			}
			return nil
		},
	}

	initCmd.Flags().StringVar(&opts.OCPVersion, "ocp-version", opts.OCPVersion, "When set, include OpenShift platform channel stable-<version> in imagesetconfig.yaml (e.g. 4.19)")
	initCmd.Flags().StringVar(&opts.Registry, "registry", opts.Registry, "Optional mirror registry hostname for values-disconnected.yaml and mirror-commands.sh")
	initCmd.Flags().StringVar(&opts.OutputDir, "output-dir", opts.OutputDir, "Directory for generated artifacts (and Git clones by default)")
	initCmd.Flags().StringVar(&opts.HelmRepo, "helm-repo", opts.HelmRepo, "OCI registry/org for Validated Patterns charts")
	initCmd.Flags().BoolVar(&opts.WriteValues, "write-values", true, "Emit values-disconnected.yaml overlay")
	initCmd.Flags().BoolVar(&opts.SkipOCIResolve, "skip-oci-resolve", false, "Skip live OCI chart version resolution and image discovery (wildcards will not be resolved)")
	initCmd.Flags().StringVar(&clusterGroups, "cluster-groups", "", "Comma-separated cluster group names (default: all)")
	initCmd.Flags().StringVar(&opts.GitRef, "git-ref", "", "Branch or tag to clone when the source is a Git URL")
	initCmd.Flags().StringVar(&opts.CloneDir, "clone-dir", "", "Directory for Git clones (default: --output-dir)")
	initCmd.Flags().BoolVar(&opts.DownloadOCMirror, "download-oc-mirror", false, "Download oc-mirror.tar.gz and oc-mirror.rhel9.tar.gz into --output-dir")
	initCmd.Flags().StringVar(&opts.ExtraConfigPath, "extra-config", "", "External YAML with operators, additionalImages, and helm.local entries to merge into imagesetconfig")

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

	var trustCA airgap.TrustCAOptions
	var trustHosts []string
	trustCmd := &cobra.Command{
		Use:   "trust-ca",
		Short: "Generate Argo CD TLS trust artifacts for a private CA",
		Long: `Generate apply-ready artifacts so OpenShift GitOps / Argo CD trusts
a CA for one or more hostnames (Gitea, mirror registry, etc.).

Does not contact the cluster. Copy the output directory to a bastion and run
./apply-trust-ca.sh with a working kubeconfig.

Artifacts:
  - argocd-tls-certs-patch.yaml
  - trusted-ca-bundle-append.pem
  - apply-trust-ca.sh
  - README.md

Examples:
  patternizer airgap trust-ca \
    --ca /home/fedora/gitea_container_certs/ca.crt \
    --host gitea-gitea.apps.airgap.rhuk.lab \
    --namespace vp-gitops \
    --output-dir ./airgap-trust

  patternizer airgap trust-ca \
    --ca /etc/pki/ca-trust/source/anchors/quay-rootCA.pem \
    --host mirror.airgap.rhuk.lab \
    --output-dir ./airgap-trust
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			trustCA.Hosts = trustHosts
			if trustCA.OutputDir != "" && !filepath.IsAbs(trustCA.OutputDir) {
				abs, err := filepath.Abs(trustCA.OutputDir)
				if err != nil {
					return err
				}
				trustCA.OutputDir = abs
			}
			res, err := airgap.GenerateTrustCA(&trustCA)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Airgap trust-ca artifacts written to %s\n", res.OutputDir)
			fmt.Fprintf(cmd.OutOrStdout(), "  namespace:    %s\n", res.Namespace)
			fmt.Fprintf(cmd.OutOrStdout(), "  repo-server: %s\n", res.RepoServer)
			fmt.Fprintf(cmd.OutOrStdout(), "  hosts:       %s\n", strings.Join(res.Hosts, ", "))
			fmt.Fprintf(cmd.OutOrStdout(), "  ca bytes:    %d\n", res.CABytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Apply on a host with oc/kubeconfig:\n  %s/apply-trust-ca.sh\n", res.OutputDir)
			return nil
		},
	}
	trustCmd.Flags().StringVar(&trustCA.CAPath, "ca", "", "Path to a PEM file or directory of .crt/.pem files (required)")
	trustCmd.Flags().StringArrayVar(&trustHosts, "host", nil, "Hostname to trust (repeatable; comma-separated allowed; no port)")
	trustCmd.Flags().StringVar(&trustCA.Namespace, "namespace", "vp-gitops", "Argo CD / OpenShift GitOps namespace")
	trustCmd.Flags().StringVar(&trustCA.OutputDir, "output-dir", "./airgap-trust", "Directory for generated artifacts")
	trustCmd.Flags().StringVar(&trustCA.RepoServer, "repo-server", "", "Repo-server deployment name (default: <namespace>-repo-server)")
	_ = trustCmd.MarkFlagRequired("ca")
	_ = trustCmd.MarkFlagRequired("host")

	var repoCreds airgap.RepoCredsOptions
	repoCredsCmd := &cobra.Command{
		Use:   "repo-creds",
		Short: "Generate Argo CD registry repo-creds Secret artifacts",
		Long: `Generate apply-ready artifacts so OpenShift GitOps / Argo CD can
authenticate to a private OCI/Helm registry (Quay mirror, Harbor, etc.).

Credentials are never hardcoded. Provide either:

  --docker-config ~/.docker/config.json   (or containers auth.json)
  --username USER --password PASS         (or --password-file)

Does not contact the cluster. Run ./apply-repo-creds.sh on a bastion with kubeconfig.

Artifacts:
  - argocd-repo-creds-secret.yaml  (mode 0600; do not commit)
  - apply-repo-creds.sh
  - README.md

Examples:
  patternizer airgap repo-creds \
    --docker-config ~/.docker/config.json \
    --url mirror.airgap.rhuk.lab:8443 \
    --namespace vp-gitops \
    --output-dir ./airgap-repo-creds

  patternizer airgap repo-creds \
    --url mirror.example.com:8443 \
    --username init \
    --password-file /path/to/registry.password \
    --output-dir ./airgap-repo-creds

Typically run after "airgap trust-ca" for the same registry so TLS and auth are both set.
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if repoCreds.OutputDir != "" && !filepath.IsAbs(repoCreds.OutputDir) {
				abs, err := filepath.Abs(repoCreds.OutputDir)
				if err != nil {
					return err
				}
				repoCreds.OutputDir = abs
			}
			res, err := airgap.GenerateRepoCreds(&repoCreds)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Airgap repo-creds artifacts written to %s\n", res.OutputDir)
			fmt.Fprintf(cmd.OutOrStdout(), "  namespace:    %s\n", res.Namespace)
			fmt.Fprintf(cmd.OutOrStdout(), "  secret:      %s\n", res.SecretName)
			fmt.Fprintf(cmd.OutOrStdout(), "  url:         %s\n", res.URL)
			fmt.Fprintf(cmd.OutOrStdout(), "  username:    %s\n", res.Username)
			fmt.Fprintf(cmd.OutOrStdout(), "  repo-server: %s\n", res.RepoServer)
			fmt.Fprintf(cmd.OutOrStdout(), "Apply on a host with oc/kubeconfig:\n  %s/apply-repo-creds.sh\n", res.OutputDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s/argocd-repo-creds-secret.yaml contains a password — do not commit it.\n", res.OutputDir)
			return nil
		},
	}
	repoCredsCmd.Flags().StringVar(&repoCreds.DockerConfig, "docker-config", "", "Path to docker/containers config.json with auths (alternative to --username)")
	repoCredsCmd.Flags().StringVar(&repoCreds.Username, "username", "", "Registry username (use with --password or --password-file)")
	repoCredsCmd.Flags().StringVar(&repoCreds.Password, "password", "", "Registry password (prefer --password-file)")
	repoCredsCmd.Flags().StringVar(&repoCreds.PasswordFile, "password-file", "", "File containing the registry password")
	repoCredsCmd.Flags().StringVar(&repoCreds.URL, "url", "", "Registry URL for Argo CD repo-creds (host:port; required with --username, selects auth with --docker-config)")
	repoCredsCmd.Flags().StringVar(&repoCreds.Namespace, "namespace", "vp-gitops", "Argo CD / OpenShift GitOps namespace")
	repoCredsCmd.Flags().StringVar(&repoCreds.OutputDir, "output-dir", "./airgap-repo-creds", "Directory for generated artifacts")
	repoCredsCmd.Flags().StringVar(&repoCreds.SecretName, "name", "", "Secret name (default: <host>-repo-creds)")
	repoCredsCmd.Flags().StringVar(&repoCreds.RepoServer, "repo-server", "", "Repo-server deployment name (default: <namespace>-repo-server)")

	var bootstrapOpts airgap.BootstrapOptions
	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap [pattern-dir]",
		Short: "Generate unified airgap trust artifacts from values-secret airgap: config",
		Long: `Read the airgap: section from values-secret.yaml (same path/value field style
as Vault secrets) and generate a single apply script for:

  - Cluster Proxy / user-ca-bundle (patterns-operator git clone TLS)
  - Argo CD TLS trust (argocd-tls-certs-cm + trusted-ca-bundle)
  - Argo CD registry repo-creds
  - Rendered values-global.yaml and variants/*/values-*.yaml
    (helmRepoUrl + CatalogSource names)

CatalogSources (pick one approach in airgap:):
  - catalogSources: { redhat: cs-..., community: cs-... }
  - ocpVersion: "4.22"  (derives oc-mirror style names)
  - discoverCatalogs: true  (or --discover-catalogs; requires oc)

Also set airgap.registry (e.g. mirror.example.com:8443) for helmRepoUrl.
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				bootstrapOpts.PatternDir = args[0]
				if !filepath.IsAbs(bootstrapOpts.PatternDir) {
					abs, err := filepath.Abs(bootstrapOpts.PatternDir)
					if err != nil {
						return err
					}
					bootstrapOpts.PatternDir = abs
				}
			}
			if bootstrapOpts.OutputDir != "" && !filepath.IsAbs(bootstrapOpts.OutputDir) {
				abs, err := filepath.Abs(bootstrapOpts.OutputDir)
				if err != nil {
					return err
				}
				bootstrapOpts.OutputDir = abs
			}
			res, err := airgap.GenerateBootstrap(&bootstrapOpts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Airgap bootstrap artifacts written to %s\n", res.OutputDir)
			fmt.Fprintf(cmd.OutOrStdout(), "  secrets file:     %s\n", res.SecretsFile)
			fmt.Fprintf(cmd.OutOrStdout(), "  argocd namespace:  %s\n", res.ArgocdNamespace)
			fmt.Fprintf(cmd.OutOrStdout(), "  clusterProxy CAs: %d\n", res.ClusterProxyCAs)
			fmt.Fprintf(cmd.OutOrStdout(), "  argocd TLS hosts: %d\n", res.ArgocdTLSHosts)
			fmt.Fprintf(cmd.OutOrStdout(), "  registries:       %d\n", res.Registries)
			if len(res.RenderedFiles) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  rendered values:  %d file(s)\n", len(res.RenderedFiles))
				if res.HelmRepoURL != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    helmRepoUrl:    %s\n", res.HelmRepoURL)
				}
				if res.CommunityCS != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    patterns CS:    %s\n", res.CommunityCS)
				}
				if res.RedHatCS != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    gitops/ACM CS:  %s\n", res.RedHatCS)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Apply on a host with oc/kubeconfig:\n  %s/apply-airgap.sh\n", res.OutputDir)
			fmt.Fprintf(cmd.OutOrStdout(), "Restore Proxy backup:\n  %s/restore-proxy-cluster.sh\n", res.OutputDir)
			return nil
		},
	}
	bootstrapCmd.Flags().StringVar(&bootstrapOpts.SecretsFile, "secrets-file", "", "Path to values-secret.yaml (default: VP resolution order)")
	bootstrapCmd.Flags().StringVar(&bootstrapOpts.OutputDir, "output-dir", "./airgap-bootstrap", "Directory for generated artifacts")
	bootstrapCmd.Flags().StringVar(&bootstrapOpts.Namespace, "namespace", "", "Override airgap.argocdNamespace")
	bootstrapCmd.Flags().StringVar(&bootstrapOpts.RepoServer, "repo-server", "", "Repo-server deployment name (default: <namespace>-repo-server)")
	bootstrapCmd.Flags().StringVar(&bootstrapOpts.PatternName, "pattern-name", "", "Pattern name for ~/values-secret-<name>.yaml lookup")
	bootstrapCmd.Flags().BoolVar(&bootstrapOpts.DiscoverCatalogs, "discover-catalogs", false, "Query the cluster (oc) for CatalogSource names")
	bootstrapCmd.Flags().BoolVar(&bootstrapOpts.SkipRenderValues, "skip-render-values", false, "Do not rewrite values-global.yaml / variants values files")
	bootstrapCmd.Flags().StringVar(&bootstrapOpts.Registry, "registry", "", "Override airgap.registry for helmRepoUrl")
	bootstrapCmd.Flags().StringVar(&bootstrapOpts.OCPVersion, "ocp-version", "", "Override airgap.ocpVersion for CatalogSource name derivation")

	airgapCmd.AddCommand(initCmd)
	airgapCmd.AddCommand(parseLogCmd)
	airgapCmd.AddCommand(trustCmd)
	airgapCmd.AddCommand(repoCredsCmd)
	airgapCmd.AddCommand(bootstrapCmd)
	return airgapCmd
}
