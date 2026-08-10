package airgap_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("DiscoverImagesFromChartDir", func() {
	It("keeps RH vault image and skips disabled injector defaults", func() {
		tmp, err := os.MkdirTemp("", "vault-values-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		Expect(os.WriteFile(filepath.Join(tmp, "Chart.yaml"), []byte(`
apiVersion: v2
name: hashicorp-vault
version: 0.1.8
`), 0o644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tmp, "templates"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "templates", "dummy.yaml"), []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: dummy
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "values.yaml"), []byte(`
vault:
  server:
    image:
      repository: "registry.connect.redhat.com/hashicorp/vault"
      tag: "1.21.4-ubi"
  injector:
    enabled: false
    image:
      repository: "hashicorp/vault-k8s"
      tag: "1.7.2"
  csi:
    enabled: false
    image:
      repository: "hashicorp/vault-csi-provider"
      tag: "1.7.0"
`), 0o644)).To(Succeed())

		Expect(os.MkdirAll(filepath.Join(tmp, "charts", "vault"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "charts", "vault", "Chart.yaml"), []byte(`
apiVersion: v2
name: vault
version: 0.30.0
`), 0o644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tmp, "charts", "vault", "templates"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "charts", "vault", "templates", "dummy.yaml"), []byte("{}\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "charts", "vault", "values.yaml"), []byte(`
server:
  image:
    repository: hashicorp/vault
    tag: 1.21.2
injector:
  enabled: true
  image:
    repository: hashicorp/vault-k8s
    tag: 1.7.2
`), 0o644)).To(Succeed())

		imgs, _, err := airgap.DiscoverImagesFromChartDir(tmp)
		Expect(err).NotTo(HaveOccurred())
		Expect(imgs).To(ContainElement("registry.connect.redhat.com/hashicorp/vault:1.21.4-ubi"))
		Expect(imgs).NotTo(ContainElement("hashicorp/vault-k8s:1.7.2"))
		Expect(imgs).NotTo(ContainElement("hashicorp/vault-csi-provider:1.7.0"))
		Expect(imgs).NotTo(ContainElement("hashicorp/vault:1.21.2"))
	})
})

var _ = Describe("isVPSourceImage filtering", func() {
	It("accepts VP registries and rejects docker hub short names", func() {
		tmp, err := os.MkdirTemp("", "filter-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)
		Expect(os.WriteFile(filepath.Join(tmp, "Chart.yaml"), []byte("apiVersion: v2\nname: t\nversion: 0.1.0\n"), 0o644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(tmp, "templates"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "templates", "x.yaml"), []byte("{}\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(tmp, "values.yaml"), []byte(`
a:
  image:
    repository: registry.redhat.io/ubi9/ubi-minimal
    tag: latest
b:
  image:
    repository: docker.io/library/nginx
    tag: latest
c:
  image:
    repository: hashicorp/vault
    tag: 1.21.2
d:
  image:
    repository: quay.io/validatedpatterns/imperative-container
    tag: v1
e:
  image:
    repository: ghcr.io/external-secrets/external-secrets
    tag: v0.10.2-ubi
`), 0o644)).To(Succeed())

		imgs, _, err := airgap.DiscoverImagesFromChartDir(tmp)
		Expect(err).NotTo(HaveOccurred())
		Expect(imgs).To(ContainElement("registry.redhat.io/ubi9/ubi-minimal:latest"))
		Expect(imgs).To(ContainElement("quay.io/validatedpatterns/imperative-container:v1"))
		Expect(imgs).To(ContainElement("ghcr.io/external-secrets/external-secrets:v0.10.2-ubi"))
		Expect(imgs).NotTo(ContainElement("docker.io/library/nginx:latest"))
		Expect(imgs).NotTo(ContainElement("hashicorp/vault:1.21.2"))
	})
})
