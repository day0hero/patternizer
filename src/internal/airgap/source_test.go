package airgap_test

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/validatedpatterns/patternizer/internal/airgap"
)

var _ = Describe("ResolvePatternSource", func() {
	It("accepts absolute paths to pattern roots", func() {
		tmp, err := os.MkdirTemp("", "src-abs-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmp)

		Expect(os.WriteFile(filepath.Join(tmp, "values-global.yaml"), []byte("global:\n  pattern: x\n"), 0o644)).To(Succeed())

		src, err := airgap.ResolvePatternSource(tmp, &airgap.Options{})
		Expect(err).NotTo(HaveOccurred())
		Expect(src.Dir).To(Equal(tmp))
		Expect(src.FromGit).To(BeFalse())
	})

	It("rejects relative paths", func() {
		_, err := airgap.ResolvePatternSource("./relative-pattern", &airgap.Options{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("absolute"))
	})

	It("detects git URLs", func() {
		Expect(airgap.IsGitURL("https://github.com/validatedpatterns/multicloud-gitops")).To(BeTrue())
		Expect(airgap.IsGitURL("git@github.com:validatedpatterns/multicloud-gitops.git")).To(BeTrue())
		Expect(airgap.IsGitURL("/home/user/patterns/multicloud-gitops")).To(BeFalse())
		Expect(airgap.IsGitURL(".")).To(BeFalse())
	})

	It("clones a local git repository URL", func() {
		if _, err := exec.LookPath("git"); err != nil {
			Skip("git not available")
		}

		bareParent, err := os.MkdirTemp("", "src-git-")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(bareParent)

		repo := filepath.Join(bareParent, "repo")
		Expect(os.MkdirAll(repo, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, "values-global.yaml"), []byte(`
global:
  pattern: cloned-pattern
main:
  clusterGroupName: hub
  multiSourceConfig:
    enabled: true
    clusterGroupChartVersion: "0.9.1"
`), 0o644)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(repo, "charts", "all", "hello-world", "templates"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, "values-hub.yaml"), []byte(`
clusterGroup:
  name: hub
  namespaces:
    hello-world:
  subscriptions: {}
  applications:
    hello-world:
      name: hello-world
      namespace: hello-world
      path: charts/all/hello-world
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, "charts", "all", "hello-world", "Chart.yaml"), []byte(`
apiVersion: v2
name: hello-world
version: 0.1.0
`), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, "charts", "all", "hello-world", "values.yaml"), []byte("image: busybox:1.36\n"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(repo, "charts", "all", "hello-world", "templates", "deploy.yaml"), []byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hello
spec:
  template:
    spec:
      containers:
      - name: c
        image: "{{ .Values.image }}"
`), 0o644)).To(Succeed())

		cmds := [][]string{
			{"git", "init"},
			{"git", "config", "user.email", "test@example.com"},
			{"git", "config", "user.name", "test"},
			{"git", "add", "."},
			{"git", "commit", "-m", "init"},
		}
		for _, c := range cmds {
			cmd := exec.Command(c[0], c[1:]...)
			cmd.Dir = repo
			out, err := cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))
		}

		outDir := filepath.Join(bareParent, "out")
		opts := airgap.Options{
			PatternSource:  "file://" + repo,
			OutputDir:      outDir,
			SkipOCIResolve: true,
			WriteValues:    true,
		}
		inv, err := airgap.Init(&opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(inv.PatternName).To(Equal("cloned-pattern"))
		Expect(inv.ResolvedDir).To(Equal(filepath.Join(outDir, "repo")))
		Expect(filepath.Join(outDir, "imagesetconfig.yaml")).To(BeAnExistingFile())
		Expect(filepath.Join(outDir, "repo", "values-global.yaml")).To(BeAnExistingFile())
		Expect(inv.LocalCharts).To(BeEmpty())
		Expect(inv.Warnings).To(ContainElement(ContainSubstring("path-based application")))
	})
})
