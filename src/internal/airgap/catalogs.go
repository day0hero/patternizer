package airgap

import "fmt"

// CatalogKind identifies an OLM catalog index.
type CatalogKind string

const (
	CatalogRedHat      CatalogKind = "redhat"
	CatalogMarketplace CatalogKind = "marketplace"
	CatalogCommunity   CatalogKind = "community"
)

// OperatorRef is a package to mirror from an OLM catalog.
type OperatorRef struct {
	Name    string
	Channel string
	Catalog CatalogKind
	Source  string
}

var defaultPackageCatalog = map[string]CatalogKind{
	"patterns-operator":                              CatalogCommunity,
	"advanced-cluster-management":                    CatalogRedHat,
	"multicluster-engine":                            CatalogRedHat,
	"openshift-gitops-operator":                      CatalogRedHat,
	"openshift-external-secrets-operator":            CatalogRedHat,
	"lvms-operator":                                  CatalogRedHat,
	"rhacs-operator":                                 CatalogRedHat,
	"odf-operator":                                   CatalogRedHat,
	"quay-operator":                                  CatalogRedHat,
	"openshift-pipelines-operator-rh":                CatalogRedHat,
	"rhtas-operator":                                 CatalogRedHat,
	"rhtpa-operator":                                 CatalogRedHat,
	"openshift-cert-manager-operator":                CatalogMarketplace,
	"rhbk-operator":                                  CatalogMarketplace,
	"openshift-zero-trust-workload-identity-manager": CatalogMarketplace,
	"compliance-operator":                            CatalogMarketplace,
}

// CatalogImage returns the catalog index image for a kind and OCP version.
func CatalogImage(kind CatalogKind, ocpVersion string) string {
	tag := "v" + ocpVersion
	switch kind {
	case CatalogMarketplace:
		return fmt.Sprintf("registry.redhat.io/redhat/redhat-marketplace-index:%s", tag)
	case CatalogCommunity:
		return fmt.Sprintf("registry.redhat.io/redhat/community-operator-index:%s", tag)
	default:
		return fmt.Sprintf("registry.redhat.io/redhat/redhat-operator-index:%s", tag)
	}
}

// CatalogSourceName returns the oc-mirror v2 CatalogSource name for a catalog image tag.
func CatalogSourceName(kind CatalogKind, ocpVersion string) string {
	switch kind {
	case CatalogMarketplace:
		return fmt.Sprintf("cs-redhat-marketplace-index-v%s", dashVersion(ocpVersion))
	case CatalogCommunity:
		return fmt.Sprintf("cs-community-operator-index-v%s", dashVersion(ocpVersion))
	default:
		return fmt.Sprintf("cs-redhat-operator-index-v%s", dashVersion(ocpVersion))
	}
}

func dashVersion(ocpVersion string) string {
	out := make([]rune, 0, len(ocpVersion))
	for _, r := range ocpVersion {
		if r == '.' {
			out = append(out, '-')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}

// LookupCatalog returns the catalog kind for a package name.
func LookupCatalog(packageName string) CatalogKind {
	if kind, ok := defaultPackageCatalog[packageName]; ok {
		return kind
	}
	return CatalogRedHat
}
