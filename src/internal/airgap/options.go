package airgap

// Options configures airgap init.
type Options struct {
	PatternSource    string
	PatternDir       string
	OutputDir        string
	OCPVersion       string
	Registry         string
	IncludePlatform  bool
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

// DefaultOptions returns default airgap init options.
func DefaultOptions() Options {
	return Options{
		OutputDir:   "./airgap-out",
		OCPVersion:  "4.19",
		Registry:    "registry.internal.disconnected.net",
		HelmRepo:    "quay.io/validatedpatterns",
		WriteValues: true,
	}
}
