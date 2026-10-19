package airgap_test

import (
	"encoding/base64"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

const bootstrapCA = `-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAKHBfJ7eBOOTSTRAP
-----END CERTIFICATE-----
`

var _ = Describe("Airgap values-secret parsing", func() {
	It("resolves certificates and registries from airgap: section", func() {
		tmp := GinkgoT().TempDir()
		caPath := filepath.Join(tmp, "ca.pem")
		Expect(os.WriteFile(caPath, []byte(bootstrapCA), 0o644)).To(Succeed())
		auth := base64.StdEncoding.EncodeToString([]byte("init:secret"))
		dockerCfg := filepath.Join(tmp, "config.json")
		Expect(os.WriteFile(dockerCfg, []byte(`{"auths":{"mirror.example.com:8443":{"auth":"`+auth+`"}}}`), 0o600)).To(Succeed())

		secrets := filepath.Join(tmp, "values-secret.yaml")
		Expect(os.WriteFile(secrets, []byte(`
version: "2.0"
secrets: []
airgap:
  argocdNamespace: vp-gitops
  certificates:
    - name: gitea
      fields:
      - name: host
        value: gitea.example.com
      - name: ca
        path: `+caPath+`
      - name: clusterProxy
        value: "true"
      - name: argocdTLS
        value: "true"
  registries:
    - name: mirror
      fields:
      - name: url
        value: mirror.example.com:8443
      - name: dockerconfig
        path: `+dockerCfg+`
`), 0o600)).To(Succeed())

		spec, err := airgap.LoadAirgapSpec(secrets)
		Expect(err).NotTo(HaveOccurred())
		certs, err := airgap.ResolveCertificates(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(certs).To(HaveLen(1))
		Expect(certs[0].Host).To(Equal("gitea.example.com"))
		Expect(certs[0].ClusterProxy).To(BeTrue())
		Expect(certs[0].ArgocdTLS).To(BeTrue())

		regs, err := airgap.ResolveRegistries(spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(regs).To(HaveLen(1))
		Expect(regs[0].URL).To(Equal("mirror.example.com:8443"))
		Expect(regs[0].DockerConfigPath).To(Equal(dockerCfg))
	})
})

var _ = Describe("GenerateBootstrap", func() {
	It("writes apply-airgap.sh with proxy backup and combined artifacts", func() {
		tmp := GinkgoT().TempDir()
		caPath := filepath.Join(tmp, "gitea.pem")
		Expect(os.WriteFile(caPath, []byte(bootstrapCA), 0o644)).To(Succeed())
		pwFile := filepath.Join(tmp, "pw")
		Expect(os.WriteFile(pwFile, []byte("regpass"), 0o600)).To(Succeed())

		patternDir := filepath.Join(tmp, "pattern")
		Expect(os.MkdirAll(patternDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(patternDir, "values-global.yaml"), []byte(`
global:
  pattern: test-pattern
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(patternDir, "values-secret.yaml"), []byte(`
version: "2.0"
airgap:
  certificates:
    - name: gitea
      fields:
      - name: host
        value: gitea.example.com
      - name: ca
        path: `+caPath+`
      - name: clusterProxy
        value: "true"
      - name: argocdTLS
        value: "true"
  registries:
    - name: mirror
      fields:
      - name: url
        value: mirror.example.com:8443
      - name: username
        value: init
      - name: password
        path: `+pwFile+`
`), 0o600)).To(Succeed())

		outDir := filepath.Join(tmp, "out")
		res, err := airgap.GenerateBootstrap(&airgap.BootstrapOptions{
			PatternDir: patternDir,
			OutputDir:  outDir,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.ClusterProxyCAs).To(Equal(1))
		Expect(res.ArgocdTLSHosts).To(Equal(1))
		Expect(res.Registries).To(Equal(1))

		Expect(filepath.Join(outDir, "apply-airgap.sh")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "restore-proxy-cluster.sh")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "argocd-tls-certs-patch.yaml")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "certs", "gitea.pem")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "secrets", "mirror-repo-creds.yaml")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "backup")).To(BeADirectory())

		script, err := os.ReadFile(filepath.Join(outDir, "apply-airgap.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(script)).To(ContainSubstring("proxy-cluster.yaml"))
		Expect(string(script)).To(ContainSubstring("user-ca-bundle"))
		Expect(string(script)).To(ContainSubstring("patterns-operator-controller-manager"))
		Expect(string(script)).To(ContainSubstring("argocd-tls-certs-cm"))
		Expect(string(script)).To(ContainSubstring("mirror-repo-creds.yaml"))
		Expect(string(script)).To(ContainSubstring("[pre|post|all]"))
		Expect(string(script)).To(ContainSubstring("RUN_PRE"))
		Expect(string(script)).To(ContainSubstring("RUN_POST"))
		Expect(string(script)).To(ContainSubstring("wait_for_cm"))
		Expect(string(script)).To(ContainSubstring("Applying Argo CD registry repo-creds"))

		restore, err := os.ReadFile(filepath.Join(outDir, "restore-proxy-cluster.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(restore)).To(ContainSubstring("proxy-cluster.yaml"))

		info, err := os.Stat(filepath.Join(outDir, "apply-airgap.sh"))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm() & 0o111).NotTo(BeZero())
	})
})
