package airgap

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// TrustCAOptions configures generate-only Argo CD TLS trust artifacts.
type TrustCAOptions struct {
	CAPath     string
	Hosts      []string
	Namespace  string
	OutputDir  string
	RepoServer string
}

// TrustCAResult summarizes generated trust-ca artifacts.
type TrustCAResult struct {
	OutputDir  string
	Hosts      []string
	Namespace  string
	RepoServer string
	CABytes    int
}

// GenerateTrustCA writes Argo CD TLS trust artifacts for the given CA and hosts.
// It does not contact the cluster; the user applies apply-trust-ca.sh with oc.
func GenerateTrustCA(opts *TrustCAOptions) (*TrustCAResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("options are required")
	}
	if strings.TrimSpace(opts.CAPath) == "" {
		return nil, fmt.Errorf("--ca is required")
	}
	if len(opts.Hosts) == 0 {
		return nil, fmt.Errorf("at least one --host is required")
	}

	ns := strings.TrimSpace(opts.Namespace)
	if ns == "" {
		ns = "vp-gitops"
	}
	outDir := strings.TrimSpace(opts.OutputDir)
	if outDir == "" {
		outDir = "./airgap-trust"
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return nil, err
	}
	outDir = absOut

	repoServer := strings.TrimSpace(opts.RepoServer)
	if repoServer == "" {
		repoServer = ns + "-repo-server"
	}

	pem, err := LoadCAPEM(opts.CAPath)
	if err != nil {
		return nil, err
	}

	hosts, err := NormalizeHosts(opts.Hosts)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	if err := writeTLSCertsPatch(outDir, ns, hosts, pem); err != nil {
		return nil, fmt.Errorf("write argocd-tls-certs-patch.yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "trusted-ca-bundle-append.pem"), []byte(pem), 0o644); err != nil {
		return nil, fmt.Errorf("write trusted-ca-bundle-append.pem: %w", err)
	}
	if err := writeApplyTrustCAScript(outDir, ns, repoServer, hosts); err != nil {
		return nil, fmt.Errorf("write apply-trust-ca.sh: %w", err)
	}
	if err := writeTrustCAReadme(outDir, ns, repoServer, hosts); err != nil {
		return nil, fmt.Errorf("write README.md: %w", err)
	}

	return &TrustCAResult{
		OutputDir:  outDir,
		Hosts:      hosts,
		Namespace:  ns,
		RepoServer: repoServer,
		CABytes:    len(pem),
	}, nil
}

// LoadCAPEM reads a PEM file or concatenates .crt/.pem files from a directory.
func LoadCAPEM(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat --ca %s: %w", path, err)
	}

	var pem string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("read --ca dir %s: %w", path, err)
		}
		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			if strings.HasSuffix(lower, ".crt") || strings.HasSuffix(lower, ".pem") {
				files = append(files, filepath.Join(path, name))
			}
		}
		sort.Strings(files)
		if len(files) == 0 {
			return "", fmt.Errorf("no .crt or .pem files found in %s", path)
		}
		var b strings.Builder
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				return "", fmt.Errorf("read %s: %w", f, err)
			}
			b.Write(data)
			if !strings.HasSuffix(string(data), "\n") {
				b.WriteByte('\n')
			}
		}
		pem = b.String()
	} else {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read --ca %s: %w", path, err)
		}
		pem = string(data)
	}

	pem = strings.TrimSpace(pem) + "\n"
	if !strings.Contains(pem, "BEGIN CERTIFICATE") {
		return "", fmt.Errorf("CA at %s does not contain a PEM certificate (missing BEGIN CERTIFICATE)", path)
	}
	return pem, nil
}

// NormalizeHosts strips schemes, paths, and ports; returns unique hostname keys.
func NormalizeHosts(raw []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, h := range raw {
		for _, part := range strings.Split(h, ",") {
			host, err := NormalizeHost(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			if host == "" {
				continue
			}
			if _, ok := seen[host]; ok {
				continue
			}
			seen[host] = struct{}{}
			out = append(out, host)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid hosts after normalization")
	}
	sort.Strings(out)
	return out, nil
}

// NormalizeHost returns a hostname suitable for argocd-tls-certs-cm keys.
func NormalizeHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	candidate := raw
	if strings.Contains(candidate, "://") {
		u, err := url.Parse(candidate)
		if err != nil {
			return "", fmt.Errorf("parse host %q: %w", raw, err)
		}
		candidate = u.Host
		if candidate == "" {
			candidate = u.Path
		}
	} else if strings.Contains(candidate, "/") {
		candidate = strings.SplitN(candidate, "/", 2)[0]
	}
	candidate = strings.TrimSpace(candidate)
	if i := strings.LastIndex(candidate, ":"); i >= 0 {
		// Strip port; avoid mangling IPv6 ([::1]:443).
		if !strings.HasPrefix(candidate, "[") {
			candidate = candidate[:i]
		} else if end := strings.Index(candidate, "]"); end >= 0 && end+1 < len(candidate) && candidate[end+1] == ':' {
			candidate = candidate[:end+1]
		}
	}
	candidate = strings.Trim(candidate, "[]")
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", fmt.Errorf("empty hostname from %q", raw)
	}
	if strings.ContainsAny(candidate, "/\\ ") {
		return "", fmt.Errorf("invalid hostname %q (from %q)", candidate, raw)
	}
	return candidate, nil
}

func writeTLSCertsPatch(outDir, namespace string, hosts []string, pem string) error {
	var b strings.Builder
	b.WriteString("# Generated by patternizer airgap trust-ca\n")
	b.WriteString("# Merge into argocd-tls-certs-cm (hostname keys only; no port).\n")
	b.WriteString("apiVersion: v1\n")
	b.WriteString("kind: ConfigMap\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: argocd-tls-certs-cm\n")
	fmt.Fprintf(&b, "  namespace: %s\n", namespace)
	b.WriteString("data:\n")
	for _, host := range hosts {
		fmt.Fprintf(&b, "  %s: |\n", host)
		for _, line := range strings.Split(strings.TrimSuffix(pem, "\n"), "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return os.WriteFile(filepath.Join(outDir, "argocd-tls-certs-patch.yaml"), []byte(b.String()), 0o644)
}

func writeApplyTrustCAScript(outDir, namespace, repoServer string, hosts []string) error {
	const script = `#!/usr/bin/env bash
# Generated by patternizer airgap trust-ca
# Applies Argo CD TLS trust for: {{.HostsList}}
# Requires: oc on PATH and a working kubeconfig (KUBECONFIG or ~/.kube/config)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
NAMESPACE="{{.Namespace}}"
REPO_SERVER="{{.RepoServer}}"
TLS_PATCH="${HERE}/argocd-tls-certs-patch.yaml"
CA_PEM="${HERE}/trusted-ca-bundle-append.pem"

if ! command -v oc >/dev/null 2>&1; then
  echo "oc is required but not found on PATH" >&2
  exit 1
fi

if [[ ! -f "${TLS_PATCH}" ]]; then
  echo "missing ${TLS_PATCH}" >&2
  exit 1
fi
if [[ ! -f "${CA_PEM}" ]]; then
  echo "missing ${CA_PEM}" >&2
  exit 1
fi

echo "==> Checking namespace ${NAMESPACE}"
oc get namespace "${NAMESPACE}" >/dev/null

echo "==> Patching ConfigMap argocd-tls-certs-cm"
oc get configmap argocd-tls-certs-cm -n "${NAMESPACE}" >/dev/null
oc patch configmap argocd-tls-certs-cm -n "${NAMESPACE}" --type=merge --patch-file="${TLS_PATCH}"

echo "==> Appending CA to ConfigMap trusted-ca-bundle (key ca-bundle.crt)"
TMP="$(mktemp)"
PATCH="$(mktemp)"
trap 'rm -f "${TMP}" "${PATCH}"' EXIT
if oc get configmap trusted-ca-bundle -n "${NAMESPACE}" >/dev/null 2>&1; then
  {
    oc get configmap trusted-ca-bundle -n "${NAMESPACE}" \
      -o jsonpath='{.data.ca-bundle\.crt}' 2>/dev/null || true
    echo
    cat "${CA_PEM}"
  } > "${TMP}"
  {
    echo "data:"
    echo "  ca-bundle.crt: |"
    sed 's/^/    /' "${TMP}"
  } > "${PATCH}"
  oc patch configmap trusted-ca-bundle -n "${NAMESPACE}" --type=merge --patch-file="${PATCH}"
else
  echo "ConfigMap trusted-ca-bundle not found in ${NAMESPACE}; creating with CA only" >&2
  oc create configmap trusted-ca-bundle -n "${NAMESPACE}" --from-file=ca-bundle.crt="${CA_PEM}"
fi

echo "==> Restarting deployment/${REPO_SERVER}"
oc rollout restart "deployment/${REPO_SERVER}" -n "${NAMESPACE}"
oc rollout status "deployment/${REPO_SERVER}" -n "${NAMESPACE}"

echo "Done. Verify with:"
echo "  oc get configmap argocd-tls-certs-cm -n ${NAMESPACE} -o yaml | head"
echo "  oc rollout status deployment/${REPO_SERVER} -n ${NAMESPACE}"
`

	tmpl, err := template.New("apply").Parse(script)
	if err != nil {
		return err
	}
	path := filepath.Join(outDir, "apply-trust-ca.sh")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, map[string]string{
		"Namespace":  namespace,
		"RepoServer": repoServer,
		"HostsList":  strings.Join(hosts, ", "),
	})
}

func writeTrustCAReadme(outDir, namespace, repoServer string, hosts []string) error {
	var b strings.Builder
	b.WriteString("# Argo CD TLS trust (generated by patternizer airgap trust-ca)\n\n")
	b.WriteString("Injects a CA into OpenShift GitOps / Argo CD so HTTPS Git hosts and OCI registries\n")
	b.WriteString("with private or self-signed certificates can be verified by the repo-server.\n\n")
	b.WriteString("## Prerequisites\n\n")
	b.WriteString("- `oc` on PATH\n")
	b.WriteString("- Working kubeconfig (`KUBECONFIG` or `~/.kube/config`)\n")
	fmt.Fprintf(&b, "- Namespace `%s` exists with Argo CD installed\n", namespace)
	fmt.Fprintf(&b, "- Deployment `%s` exists\n\n", repoServer)
	b.WriteString("## Hosts\n\n")
	for _, h := range hosts {
		fmt.Fprintf(&b, "- `%s`\n", h)
	}
	b.WriteString("\n## Apply\n\n")
	b.WriteString("On a host that can reach the cluster (e.g. bastion):\n\n")
	b.WriteString("```bash\n")
	b.WriteString("./apply-trust-ca.sh\n")
	b.WriteString("```\n\n")
	b.WriteString("This will:\n\n")
	b.WriteString("1. Merge `argocd-tls-certs-patch.yaml` into `argocd-tls-certs-cm`\n")
	b.WriteString("2. Append `trusted-ca-bundle-append.pem` into `trusted-ca-bundle`\n")
	fmt.Fprintf(&b, "3. Roll out restart `deployment/%s`\n\n", repoServer)
	b.WriteString("## Confirm\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "oc get configmap argocd-tls-certs-cm -n %s -o yaml\n", namespace)
	b.WriteString("# From repo-server, HTTPS to the trusted host should not return x509 errors\n")
	fmt.Fprintf(&b, "oc rollout status deployment/%s -n %s\n", repoServer, namespace)
	b.WriteString("```\n\n")
	b.WriteString("ConfigMap keys must be hostnames only (no port). Re-run `patternizer airgap trust-ca`\n")
	b.WriteString("to regenerate after rotating CAs.\n")
	return os.WriteFile(filepath.Join(outDir, "README.md"), []byte(b.String()), 0o644)
}
