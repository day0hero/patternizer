package airgap

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
	// Bundle runs oc-mirror into OutputDir after generating imagesetconfig.yaml.
	Bundle        bool
	ExtraWarnings []string
}

func DefaultOptions() Options {
	return Options{
		OutputDir:   "./airgap-out",
		OCPVersion:  "4.19",
		Registry:    "registry.internal.disconnected.net",
		HelmRepo:    "quay.io/validatedpatterns",
		WriteValues: true,
	}
}
