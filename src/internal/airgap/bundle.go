package airgap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const connectRedHatRegistriesD = `docker:
  registry.connect.redhat.com:
    use-sigstore-attachments: false
`

// WriteConnectRedHatRegistriesD writes a registries.d snippet that disables
// sigstore signature attachments for registry.connect.redhat.com.
// Returns the registries.d directory path for use with oc-mirror --registries.d.
func WriteConnectRedHatRegistriesD(outputDir string) (string, error) {
	dir := filepath.Join(outputDir, "registries.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "registry.connect.redhat.com.yaml")
	if err := os.WriteFile(path, []byte(connectRedHatRegistriesD), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

// RunOCMirror mirrors using an ImageSetConfiguration into a local file:// directory.
// It disables sigstore attachments for registry.connect.redhat.com via --registries.d.
func RunOCMirror(configPath, outputDir string) error {
	configAbs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(configAbs); err != nil {
		return fmt.Errorf("imageset config %q: %w", configAbs, err)
	}

	outAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		return err
	}

	registriesD, err := WriteConnectRedHatRegistriesD(outAbs)
	if err != nil {
		return fmt.Errorf("write registries.d: %w", err)
	}

	bin, err := exec.LookPath("oc-mirror")
	if err != nil {
		return fmt.Errorf("oc-mirror not found in PATH: %w", err)
	}

	dest := "file://" + outAbs
	cmd := exec.Command(bin,
		"--config="+configAbs,
		dest,
		"--v2",
		"--registries.d="+registriesD,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc-mirror %s: %w", strings.Join(cmd.Args[1:], " "), err)
	}
	return nil
}
