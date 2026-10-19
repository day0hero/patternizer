package airgap

// FrameworkImages returns images always mirrored for the Validated Patterns install path.
func FrameworkImages(helmRepo string) []string {
	return []string{
		fmtImage(helmRepo, "utility-container", "latest"),
		fmtImage(helmRepo, "imperative-container", "v1"),
	}
}

// FrameworkOperators returns operators always included for pattern install.
func FrameworkOperators() []OperatorRef {
	return []OperatorRef{
		{Name: "patterns-operator", Catalog: CatalogCommunity},
		{Name: "openshift-gitops-operator", Catalog: CatalogRedHat},
	}
}

// ACMCompanionOperators returns operators added when ACM is subscribed.
func ACMCompanionOperators() []OperatorRef {
	return []OperatorRef{
		{Name: "multicluster-engine", Catalog: CatalogRedHat},
	}
}

func fmtImage(repo, name, tag string) string {
	return repo + "/" + name + ":" + tag
}
