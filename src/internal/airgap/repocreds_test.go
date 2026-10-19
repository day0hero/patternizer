package airgap_test

import (
	"encoding/base64"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("NormalizeRegistryURL", func() {
	DescribeTable("normalizes registry URLs",
		func(in, want string) {
			Expect(airgap.NormalizeRegistryURL(in)).To(Equal(want))
		},
		Entry("host:port", "mirror.example.com:8443", "mirror.example.com:8443"),
		Entry("strips https", "https://mirror.example.com:8443", "mirror.example.com:8443"),
		Entry("keeps path", "https://mirror.example.com:8443/validatedpatterns", "mirror.example.com:8443/validatedpatterns"),
		Entry("empty", "", ""),
	)
})

var _ = Describe("ResolveRegistryCredential", func() {
	var tmp string

	BeforeEach(func() {
		tmp = GinkgoT().TempDir()
	})

	It("resolves from username and password-file", func() {
		pwFile := filepath.Join(tmp, "pw")
		Expect(os.WriteFile(pwFile, []byte("s3cret\n"), 0o600)).To(Succeed())
		cred, err := airgap.ResolveRegistryCredential(&airgap.RepoCredsOptions{
			Username:     "init",
			PasswordFile: pwFile,
			URL:          "https://mirror.example.com:8443/",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(cred.Username).To(Equal("init"))
		Expect(cred.Password).To(Equal("s3cret"))
		Expect(cred.URL).To(Equal("mirror.example.com:8443"))
	})

	It("resolves from docker config with single auth", func() {
		auth := base64.StdEncoding.EncodeToString([]byte("init:fromconfig"))
		cfg := filepath.Join(tmp, "config.json")
		Expect(os.WriteFile(cfg, []byte(`{"auths":{"mirror.example.com:8443":{"auth":"`+auth+`"}}}`), 0o600)).To(Succeed())
		cred, err := airgap.ResolveRegistryCredential(&airgap.RepoCredsOptions{
			DockerConfig: cfg,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(cred.Username).To(Equal("init"))
		Expect(cred.Password).To(Equal("fromconfig"))
		Expect(cred.URL).To(Equal("mirror.example.com:8443"))
	})

	It("selects docker config auth by --url when multiple exist", func() {
		a1 := base64.StdEncoding.EncodeToString([]byte("u1:p1"))
		a2 := base64.StdEncoding.EncodeToString([]byte("u2:p2"))
		cfg := filepath.Join(tmp, "config.json")
		Expect(os.WriteFile(cfg, []byte(`{"auths":{
			"quay.io":{"auth":"`+a1+`"},
			"mirror.example.com:8443":{"auth":"`+a2+`"}
		}}`), 0o600)).To(Succeed())
		cred, err := airgap.ResolveRegistryCredential(&airgap.RepoCredsOptions{
			DockerConfig: cfg,
			URL:          "mirror.example.com:8443",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(cred.Username).To(Equal("u2"))
		Expect(cred.Password).To(Equal("p2"))
	})

	It("rejects mixing docker-config and username", func() {
		_, err := airgap.ResolveRegistryCredential(&airgap.RepoCredsOptions{
			DockerConfig: "/tmp/x",
			Username:     "u",
			Password:     "p",
			URL:          "r.example.com",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not both"))
	})

	It("requires url with username", func() {
		_, err := airgap.ResolveRegistryCredential(&airgap.RepoCredsOptions{
			Username: "u",
			Password: "p",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("--url"))
	})
})

var _ = Describe("GenerateRepoCreds", func() {
	It("writes secret YAML, apply script, and README", func() {
		tmp := GinkgoT().TempDir()
		outDir := filepath.Join(tmp, "out")
		res, err := airgap.GenerateRepoCreds(&airgap.RepoCredsOptions{
			Username:  "init",
			Password:  "p@ss\"word",
			URL:       "mirror.airgap.rhuk.lab:8443",
			Namespace: "vp-gitops",
			OutputDir: outDir,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.SecretName).To(Equal("mirror.airgap.rhuk.lab-repo-creds"))
		Expect(res.URL).To(Equal("mirror.airgap.rhuk.lab:8443"))
		Expect(res.Username).To(Equal("init"))
		Expect(res.RepoServer).To(Equal("vp-gitops-repo-server"))

		secretPath := filepath.Join(outDir, "argocd-repo-creds-secret.yaml")
		Expect(secretPath).To(BeAnExistingFile())
		info, err := os.Stat(secretPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm() & 0o077).To(BeZero())

		raw, err := os.ReadFile(secretPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring("argocd.argoproj.io/secret-type: repo-creds"))
		Expect(string(raw)).To(ContainSubstring("url: \"mirror.airgap.rhuk.lab:8443\""))
		Expect(string(raw)).To(ContainSubstring("username: \"init\""))
		Expect(string(raw)).To(ContainSubstring(`password: "p@ss\"word"`))
		Expect(string(raw)).NotTo(ContainSubstring("quay.io"))

		script, err := os.ReadFile(filepath.Join(outDir, "apply-repo-creds.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(script)).To(HavePrefix("#!/usr/bin/env bash"))
		Expect(string(script)).To(ContainSubstring("argocd-repo-creds-secret.yaml"))
		Expect(string(script)).To(ContainSubstring("rollout restart"))

		scriptInfo, err := os.Stat(filepath.Join(outDir, "apply-repo-creds.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(scriptInfo.Mode().Perm() & 0o111).NotTo(BeZero())

		readme, err := os.ReadFile(filepath.Join(outDir, "README.md"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(readme)).To(ContainSubstring("Do not commit"))
		Expect(string(readme)).To(ContainSubstring("trust-ca"))
	})

	It("respects custom secret name", func() {
		tmp := GinkgoT().TempDir()
		res, err := airgap.GenerateRepoCreds(&airgap.RepoCredsOptions{
			Username:   "u",
			Password:   "p",
			URL:        "reg.example.com:5000",
			SecretName: "my-mirror-creds",
			OutputDir:  filepath.Join(tmp, "out"),
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.SecretName).To(Equal("my-mirror-creds"))
	})
})
