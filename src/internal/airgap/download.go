package airgap

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const openshiftClientsBase = "https://mirror.openshift.com/pub/openshift-v4/clients/ocp"

// DownloadOCMirrorTools downloads oc-mirror archives for the given OCP version.
func DownloadOCMirrorTools(ocpVersion, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	baseURL, err := resolveClientsBaseURL(ocpVersion)
	if err != nil {
		return err
	}

	for _, name := range []string{"oc-mirror.tar.gz", "oc-mirror.rhel9.tar.gz"} {
		url := strings.TrimSuffix(baseURL, "/") + "/" + name
		dest := filepath.Join(destDir, name)
		if err := downloadFile(url, dest); err != nil {
			return fmt.Errorf("download %s: %w", name, err)
		}
	}
	return nil
}

func resolveClientsBaseURL(ocpVersion string) (string, error) {
	candidates := []string{}
	if ocpVersion != "" {
		candidates = append(candidates, openshiftClientsBase+"/stable-"+ocpVersion)
	}
	candidates = append(candidates, openshiftClientsBase+"/stable")

	for _, base := range candidates {
		probe := strings.TrimSuffix(base, "/") + "/oc-mirror.tar.gz"
		resp, err := http.Head(probe)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return base, nil
		}
	}
	return "", fmt.Errorf("could not find oc-mirror.tar.gz under stable-%s or stable", ocpVersion)
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %s", url, resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return err
	}
	return nil
}
