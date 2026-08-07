package airgap

// FrameworkImages are always mirrored for the Validated Patterns install path.
// These are not declared in pattern application charts; they come from pattern.sh / install flow.
func FrameworkImages(helmRepo string) []string {
	return []string{
		fmtImage(helmRepo, "utility-container", "latest"),
		fmtImage(helmRepo, "imperative-container", "v1"),
		fmtImage(helmRepo, "pattern-install", "0.0.11"),
	}
}

// FrameworkOperators are always included for pattern install / GitOps / hub ACM patterns.
func FrameworkOperators() []OperatorRef {
	return []OperatorRef{
		{Name: "patterns-operator", Catalog: CatalogCommunity},
		{Name: "openshift-gitops-operator", Catalog: CatalogRedHat},
	}
}

// ACMCompanionOperators are added when advanced-cluster-management is subscribed.
func ACMCompanionOperators() []OperatorRef {
	return []OperatorRef{
		{Name: "multicluster-engine", Catalog: CatalogRedHat},
	}
}

func fmtImage(repo, name, tag string) string {
	return repo + "/" + name + ":" + tag
}
