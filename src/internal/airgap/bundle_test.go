package airgap_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("Airgap Bundle (oc-mirror)", func() {
	It("writes registries.d to disable connect.redhat.com signatures", func() {
		tmp, err := os.MkdirTemp("", "airgap-bundle-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		dir, err := airgap.WriteConnectRedHatRegistriesD(tmp)
		Expect(err).NotTo(HaveOccurred())
		Expect(dir).To(Equal(filepath.Join(tmp, "registries.d")))

		raw, err := os.ReadFile(filepath.Join(dir, "registry.connect.redhat.com.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring("registry.connect.redhat.com:"))
		Expect(string(raw)).To(ContainSubstring("use-sigstore-attachments: false"))
	})

	It("errors when the config file is missing", func() {
		tmp, err := os.MkdirTemp("", "airgap-bundle-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		err = airgap.RunOCMirror(filepath.Join(tmp, "missing.yaml"), filepath.Join(tmp, "out"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("imageset config"))
	})

	It("writes registries.d before failing when oc-mirror is missing", func() {
		tmp, err := os.MkdirTemp("", "airgap-bundle-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		cfg := filepath.Join(tmp, "imagesetconfig.yaml")
		Expect(os.WriteFile(cfg, []byte("kind: ImageSetConfiguration\n"), 0o644)).To(Succeed())
		outDir := filepath.Join(tmp, "out")

		origPath := os.Getenv("PATH")
		_ = os.Setenv("PATH", tmp)
		defer func() { _ = os.Setenv("PATH", origPath) }()

		err = airgap.RunOCMirror(cfg, outDir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("oc-mirror"))
		Expect(filepath.Join(outDir, "registries.d", "registry.connect.redhat.com.yaml")).To(BeAnExistingFile())
	})
})
