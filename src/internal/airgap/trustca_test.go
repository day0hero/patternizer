package airgap_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

const samplePEM = `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHBfJ7eEXAMPLE
-----END CERTIFICATE-----
`

var _ = Describe("NormalizeHost", func() {
	DescribeTable("normalizes host inputs",
		func(in, want string) {
			got, err := airgap.NormalizeHost(in)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("bare hostname", "gitea.example.com", "gitea.example.com"),
		Entry("strips https URL", "https://gitea.example.com/org/repo", "gitea.example.com"),
		Entry("strips port from host:port", "mirror.example.com:8443", "mirror.example.com"),
		Entry("strips port from URL", "https://mirror.example.com:8443/v2/", "mirror.example.com"),
		Entry("empty", "", ""),
	)

	It("rejects invalid hostnames", func() {
		_, err := airgap.NormalizeHost("https://")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("NormalizeHosts", func() {
	It("dedupes, splits commas, and sorts", func() {
		hosts, err := airgap.NormalizeHosts([]string{
			"b.example.com",
			"https://a.example.com/path",
			"b.example.com,c.example.com:443",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(hosts).To(Equal([]string{"a.example.com", "b.example.com", "c.example.com"}))
	})
})

var _ = Describe("LoadCAPEM", func() {
	var tmp string

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
	})

	It("loads a single PEM file", func() {
		path := filepath.Join(tmp, "ca.crt")
		Expect(os.WriteFile(path, []byte(samplePEM), 0o644)).To(Succeed())
		pem, err := airgap.LoadCAPEM(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(pem).To(ContainSubstring("BEGIN CERTIFICATE"))
		Expect(pem).To(HaveSuffix("\n"))
	})

	It("concatenates .crt and .pem files from a directory in sorted order", func() {
		dir := filepath.Join(tmp, "certs")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "b.pem"), []byte("-----BEGIN CERTIFICATE-----\nB\n-----END CERTIFICATE-----\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "a.crt"), []byte("-----BEGIN CERTIFICATE-----\nA\n-----END CERTIFICATE-----\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore"), 0o644)).To(Succeed())

		pem, err := airgap.LoadCAPEM(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(pem).To(HavePrefix("-----BEGIN CERTIFICATE-----\nA\n"))
		Expect(pem).To(ContainSubstring("-----BEGIN CERTIFICATE-----\nB\n"))
	})

	It("rejects non-PEM content", func() {
		path := filepath.Join(tmp, "not-a-cert.txt")
		Expect(os.WriteFile(path, []byte("hello"), 0o644)).To(Succeed())
		_, err := airgap.LoadCAPEM(path)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("BEGIN CERTIFICATE"))
	})

	It("rejects an empty cert directory", func() {
		dir := filepath.Join(tmp, "empty")
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		_, err := airgap.LoadCAPEM(dir)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no .crt or .pem"))
	})
})

var _ = Describe("GenerateTrustCA", func() {
	It("writes patch YAML, PEM copy, apply script, and README", func() {
		tmp := GinkgoT().TempDir()
		caPath := filepath.Join(tmp, "ca.crt")
		Expect(os.WriteFile(caPath, []byte(samplePEM), 0o644)).To(Succeed())
		outDir := filepath.Join(tmp, "out")

		res, err := airgap.GenerateTrustCA(&airgap.TrustCAOptions{
			CAPath:    caPath,
			Hosts:     []string{"https://gitea.apps.example.com/repo", "mirror.example.com:8443"},
			Namespace: "vp-gitops",
			OutputDir: outDir,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Namespace).To(Equal("vp-gitops"))
		Expect(res.RepoServer).To(Equal("vp-gitops-repo-server"))
		Expect(res.Hosts).To(Equal([]string{"gitea.apps.example.com", "mirror.example.com"}))

		Expect(filepath.Join(outDir, "argocd-tls-certs-patch.yaml")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "trusted-ca-bundle-append.pem")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "apply-trust-ca.sh")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "README.md")).To(BeAnExistingFile())

		patch, err := os.ReadFile(filepath.Join(outDir, "argocd-tls-certs-patch.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(patch)).To(ContainSubstring("name: argocd-tls-certs-cm"))
		Expect(string(patch)).To(ContainSubstring("namespace: vp-gitops"))
		Expect(string(patch)).To(ContainSubstring("gitea.apps.example.com: |"))
		Expect(string(patch)).To(ContainSubstring("mirror.example.com: |"))
		Expect(string(patch)).To(ContainSubstring("BEGIN CERTIFICATE"))
		Expect(string(patch)).NotTo(ContainSubstring(":8443"))

		pemCopy, err := os.ReadFile(filepath.Join(outDir, "trusted-ca-bundle-append.pem"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(pemCopy)).To(ContainSubstring("BEGIN CERTIFICATE"))

		script, err := os.ReadFile(filepath.Join(outDir, "apply-trust-ca.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(script)).To(HavePrefix("#!/usr/bin/env bash"))
		Expect(string(script)).To(ContainSubstring("NAMESPACE=\"vp-gitops\""))
		Expect(string(script)).To(ContainSubstring("REPO_SERVER=\"vp-gitops-repo-server\""))
		Expect(string(script)).To(ContainSubstring("argocd-tls-certs-cm"))
		Expect(string(script)).To(ContainSubstring("trusted-ca-bundle"))
		Expect(string(script)).To(ContainSubstring("rollout restart"))

		info, err := os.Stat(filepath.Join(outDir, "apply-trust-ca.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm() & 0o111).NotTo(BeZero())

		readme, err := os.ReadFile(filepath.Join(outDir, "README.md"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(readme)).To(ContainSubstring("apply-trust-ca.sh"))
		Expect(string(readme)).To(ContainSubstring("gitea.apps.example.com"))
	})

	It("defaults repo-server from namespace", func() {
		tmp := GinkgoT().TempDir()
		caPath := filepath.Join(tmp, "ca.pem")
		Expect(os.WriteFile(caPath, []byte(samplePEM), 0o644)).To(Succeed())
		res, err := airgap.GenerateTrustCA(&airgap.TrustCAOptions{
			CAPath:     caPath,
			Hosts:      []string{"git.example.com"},
			Namespace:  "openshift-gitops",
			OutputDir:  filepath.Join(tmp, "out"),
			RepoServer: "",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RepoServer).To(Equal("openshift-gitops-repo-server"))
	})

	It("requires ca and host", func() {
		_, err := airgap.GenerateTrustCA(&airgap.TrustCAOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("--ca"))
	})
})
