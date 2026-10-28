package airgap

// Options configures airgap init.
type Options struct {
	PatternSource string
	PatternDir    string
	OutputDir     string
	// OCPVersion, when set, adds mirror.platform with channel stable-<version>.
	OCPVersion string
	// Registry is optional; used in values-disconnected.yaml and mirror-commands.sh when set.
	Registry         string
	ClusterGroups    []string
	WriteValues      bool
	HelmRepo         string
	SkipOCIResolve   bool
	GitRef           string
	CloneDir         string
	DownloadOCMirror bool
	ExtraConfigPath  string
	ExtraWarnings    []string
}

// defaultOLMCatalogVersion tags pattern-discovered OLM catalog indexes when
// --ocp-version is omitted (that flag only controls the OpenShift platform channel).
const defaultOLMCatalogVersion = "4.19"

// DefaultOptions returns default airgap init options.
func DefaultOptions() Options {
	return Options{
		OutputDir:   "./airgap-out",
		HelmRepo:    "quay.io/validatedpatterns",
		WriteValues: true,
	}
}
