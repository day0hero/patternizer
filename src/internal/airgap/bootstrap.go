package airgap

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// GenerateBootstrap writes a single airgap apply bundle from values-secret airgap: config.
func GenerateBootstrap(opts *BootstrapOptions) (*BootstrapResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("options are required")
	}

	patternDir := strings.TrimSpace(opts.PatternDir)
	if patternDir != "" {
		abs, err := filepath.Abs(patternDir)
		if err != nil {
			return nil, err
		}
		patternDir = abs
	}

	patternName := strings.TrimSpace(opts.PatternName)
	if patternName == "" && patternDir != "" {
		patternName = PatternNameFromDir(patternDir)
	}

	secretsPath, err := ResolveValuesSecretPath(patternDir, patternName, opts.SecretsFile)
	if err != nil {
		return nil, err
	}

	spec, err := LoadAirgapSpec(secretsPath)
	if err != nil {
		return nil, err
	}

	certs, err := ResolveCertificates(spec)
	if err != nil {
		return nil, err
	}
	regs, err := ResolveRegistries(spec)
	if err != nil {
		return nil, err
	}

	wantRender := !opts.SkipRenderValues && (spec.WantsRenderValues() || opts.DiscoverCatalogs || opts.Registry != "" || opts.OCPVersion != "")
	if len(certs) == 0 && len(regs) == 0 && !wantRender {
		return nil, fmt.Errorf("%s airgap: section has no certificates, registries, or disconnected values settings", secretsPath)
	}

	ns := strings.TrimSpace(opts.Namespace)
	if ns == "" {
		ns = strings.TrimSpace(spec.ArgocdNamespace)
	}
	if ns == "" {
		ns = "vp-gitops"
	}
	poNS := strings.TrimSpace(spec.PatternsOperatorNamespace)
	if poNS == "" {
		poNS = "patterns-operator"
	}
	repoServer := strings.TrimSpace(opts.RepoServer)
	if repoServer == "" {
		repoServer = ns + "-repo-server"
	}

	outDir := strings.TrimSpace(opts.OutputDir)
	if outDir == "" {
		outDir = "./airgap-bootstrap"
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return nil, err
	}
	outDir = absOut

	if err := os.MkdirAll(filepath.Join(outDir, "backup"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "certs"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(outDir, "secrets"), 0o700); err != nil {
		return nil, err
	}

	var proxyCAFiles []string
	var tlsHosts []string
	hostCA := map[string]string{} // host -> pem content for argocd

	for _, c := range certs {
		pem, err := LoadCAPEM(c.CAPath)
		if err != nil {
			return nil, fmt.Errorf("certificate %q: %w", c.Name, err)
		}
		dst := filepath.Join(outDir, "certs", sanitizeFileName(c.Name)+".pem")
		if err := os.WriteFile(dst, []byte(pem), 0o644); err != nil {
			return nil, err
		}
		if c.ClusterProxy {
			proxyCAFiles = append(proxyCAFiles, filepath.Base(dst))
		}
		if c.ArgocdTLS {
			host, err := NormalizeHost(c.Host)
			if err != nil {
				return nil, fmt.Errorf("certificate %q host: %w", c.Name, err)
			}
			tlsHosts = append(tlsHosts, host)
			hostCA[host] = pem
		}
	}

	if len(hostCA) > 0 {
		if err := writeMergedTLSCertsPatch(outDir, ns, hostCA); err != nil {
			return nil, err
		}
		var combined strings.Builder
		seen := map[string]struct{}{}
		for _, host := range tlsHosts {
			pem := hostCA[host]
			if _, ok := seen[pem]; ok {
				continue
			}
			seen[pem] = struct{}{}
			combined.WriteString(pem)
			if !strings.HasSuffix(pem, "\n") {
				combined.WriteByte('\n')
			}
		}
		if err := os.WriteFile(filepath.Join(outDir, "trusted-ca-bundle-append.pem"), []byte(combined.String()), 0o644); err != nil {
			return nil, err
		}
	}

	var secretNames []string
	for _, r := range regs {
		cred, err := resolveRegistryFromResolved(&r)
		if err != nil {
			return nil, fmt.Errorf("registry %q: %w", r.Name, err)
		}
		secretName := defaultRepoCredsSecretName(cred.URL)
		if r.Name != "" {
			secretName = sanitizeFileName(r.Name) + "-repo-creds"
		}
		path := filepath.Join(outDir, "secrets", secretName+".yaml")
		if err := writeRepoCredsSecret(path, ns, secretName, cred); err != nil {
			return nil, err
		}
		secretNames = append(secretNames, secretName)
	}

	result := &BootstrapResult{
		OutputDir:       outDir,
		SecretsFile:     secretsPath,
		ArgocdNamespace: ns,
		ClusterProxyCAs: len(proxyCAFiles),
		ArgocdTLSHosts:  len(hostCA),
		Registries:      len(secretNames),
	}

	if wantRender {
		if patternDir == "" {
			return nil, fmt.Errorf("pattern dir is required to render values (pass the pattern path to airgap bootstrap)")
		}
		dcfg, err := ResolveDisconnectedValues(spec, opts)
		if err != nil {
			return nil, err
		}
		rendered, err := RenderPatternValues(patternDir, outDir, dcfg)
		if err != nil {
			return nil, fmt.Errorf("render values: %w", err)
		}
		result.RenderedFiles = rendered
		result.HelmRepoURL = dcfg.HelmRepoURL
		result.RedHatCS = dcfg.RedHatCS
		result.CommunityCS = dcfg.CommunityCS
	}

	if err := writeApplyAirgapScript(outDir, &applyAirgapData{
		ArgocdNamespace:           ns,
		PatternsOperatorNamespace: poNS,
		RepoServer:                repoServer,
		ProxyCAFiles:              proxyCAFiles,
		HasArgocdTLS:              len(hostCA) > 0,
		SecretNames:               secretNames,
	}); err != nil {
		return nil, err
	}
	if err := writeRestoreProxyScript(outDir); err != nil {
		return nil, err
	}
	if err := writeBootstrapReadme(outDir, secretsPath, ns, poNS, certs, regs, result); err != nil {
		return nil, err
	}

	return result, nil
}

func resolveRegistryFromResolved(r *ResolvedRegistry) (*RegistryCredential, error) {
	opts := &RepoCredsOptions{
		URL:          r.URL,
		DockerConfig: r.DockerConfigPath,
		Username:     r.Username,
		Password:     r.Password,
		PasswordFile: r.PasswordPath,
	}
	return ResolveRegistryCredential(opts)
}

func writeMergedTLSCertsPatch(outDir, namespace string, hostCA map[string]string) error {
	hosts := make([]string, 0, len(hostCA))
	for h := range hostCA {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	var b strings.Builder
	b.WriteString("# Generated by patternizer airgap bootstrap\n")
	b.WriteString("apiVersion: v1\n")
	b.WriteString("kind: ConfigMap\n")
	b.WriteString("metadata:\n")
	b.WriteString("  name: argocd-tls-certs-cm\n")
	fmt.Fprintf(&b, "  namespace: %s\n", namespace)
	b.WriteString("data:\n")
	for _, host := range hosts {
		pem := hostCA[host]
		fmt.Fprintf(&b, "  %s: |\n", host)
		for _, line := range strings.Split(strings.TrimSuffix(pem, "\n"), "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return os.WriteFile(filepath.Join(outDir, "argocd-tls-certs-patch.yaml"), []byte(b.String()), 0o644)
}

type applyAirgapData struct {
	ArgocdNamespace           string
	PatternsOperatorNamespace string
	RepoServer                string
	ProxyCAFiles              []string
	HasArgocdTLS              bool
	SecretNames               []string
}

func writeApplyAirgapScript(outDir string, d *applyAirgapData) error {
	const script = `#!/usr/bin/env bash
# Generated by patternizer airgap bootstrap
# Usage: ./apply-airgap.sh [pre|post|all]
#   pre  — cluster Proxy / user-ca-bundle (before make install)
#   post — Argo CD TLS + repo-creds (after vp-gitops exists)
#   all  — both (default)
# Requires: oc on PATH and a working kubeconfig (KUBECONFIG or ~/.kube/config)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BACKUP_DIR="${HERE}/backup"
ARGOCD_NS="{{.ArgocdNamespace}}"
PATTERNS_NS="{{.PatternsOperatorNamespace}}"
REPO_SERVER="{{.RepoServer}}"
PROXY_BACKUP="${BACKUP_DIR}/proxy-cluster.yaml"
USER_CA_BACKUP="${BACKUP_DIR}/user-ca-bundle.yaml"
PHASE="${1:-all}"
WAIT_SECONDS="${AIRGAP_WAIT_SECONDS:-600}"
WAIT_INTERVAL="${AIRGAP_WAIT_INTERVAL:-10}"

PROXY_CA_FILES=(
{{range .ProxyCAFiles}}  "{{.}}"
{{end}})
HAS_ARGOCD_TLS={{if .HasArgocdTLS}}1{{else}}0{{end}}
SECRET_FILES=(
{{range .SecretNames}}  "{{.}}.yaml"
{{end}})

RUN_PRE=0
RUN_POST=0
case "${PHASE}" in
  pre)  RUN_PRE=1 ;;
  post) RUN_POST=1 ;;
  all)  RUN_PRE=1; RUN_POST=1 ;;
  *)
    echo "usage: $0 [pre|post|all]" >&2
    exit 1
    ;;
esac

if ! command -v oc >/dev/null 2>&1; then
  echo "oc is required but not found on PATH" >&2
  exit 1
fi

mkdir -p "${BACKUP_DIR}"

wait_for_ns() {
  local ns="$1"
  local elapsed=0
  echo "==> Waiting up to ${WAIT_SECONDS}s for namespace/${ns}"
  while (( elapsed < WAIT_SECONDS )); do
    if oc get namespace "${ns}" >/dev/null 2>&1; then
      echo "    namespace/${ns} is present"
      return 0
    fi
    sleep "${WAIT_INTERVAL}"
    elapsed=$((elapsed + WAIT_INTERVAL))
  done
  echo "timed out waiting for namespace/${ns}" >&2
  return 1
}

wait_for_cm() {
  local ns="$1"
  local name="$2"
  local elapsed=0
  echo "==> Waiting up to ${WAIT_SECONDS}s for configmap/${name} in ${ns}"
  while (( elapsed < WAIT_SECONDS )); do
    if oc get configmap "${name}" -n "${ns}" >/dev/null 2>&1; then
      echo "    configmap/${name} is present"
      return 0
    fi
    sleep "${WAIT_INTERVAL}"
    elapsed=$((elapsed + WAIT_INTERVAL))
  done
  echo "timed out waiting for configmap/${name} in ${ns}" >&2
  return 1
}

wait_for_deploy() {
  local ns="$1"
  local name="$2"
  local elapsed=0
  echo "==> Waiting up to ${WAIT_SECONDS}s for deployment/${name} in ${ns}"
  while (( elapsed < WAIT_SECONDS )); do
    if oc get deployment "${name}" -n "${ns}" >/dev/null 2>&1; then
      echo "    deployment/${name} is present"
      return 0
    fi
    sleep "${WAIT_INTERVAL}"
    elapsed=$((elapsed + WAIT_INTERVAL))
  done
  echo "    warning: deployment/${name} not found in ${ns} within ${WAIT_SECONDS}s" >&2
  return 1
}

if (( RUN_PRE )); then
  if ((${#PROXY_CA_FILES[@]} > 0)); then
    echo "==> [pre] Backing up proxy/cluster (original preserved at ${PROXY_BACKUP})"
    if [[ ! -f "${PROXY_BACKUP}" ]]; then
      oc get proxy cluster -o yaml > "${PROXY_BACKUP}"
      echo "    wrote first-time backup ${PROXY_BACKUP}"
    else
      echo "    keeping existing first-time backup ${PROXY_BACKUP}"
    fi
    oc get proxy cluster -o yaml > "${BACKUP_DIR}/proxy-cluster-before-apply.yaml"

    echo "==> [pre] Backing up ConfigMap user-ca-bundle (if present)"
    if oc get configmap user-ca-bundle -n openshift-config >/dev/null 2>&1; then
      if [[ ! -f "${USER_CA_BACKUP}" ]]; then
        oc get configmap user-ca-bundle -n openshift-config -o yaml > "${USER_CA_BACKUP}"
      fi
      oc get configmap user-ca-bundle -n openshift-config -o yaml > "${BACKUP_DIR}/user-ca-bundle-before-apply.yaml"
    fi

    echo "==> [pre] Merging CAs into openshift-config/user-ca-bundle and enabling Proxy trustedCA"
    TMP_CA="$(mktemp)"
    trap 'rm -f "${TMP_CA}"' EXIT
    {
      oc get configmap user-ca-bundle -n openshift-config -o jsonpath='{.data.ca-bundle\.crt}' 2>/dev/null || true
      echo
      for f in "${PROXY_CA_FILES[@]}"; do
        cat "${HERE}/certs/${f}"
        echo
      done
    } > "${TMP_CA}"

    oc create configmap user-ca-bundle -n openshift-config \
      --from-file=ca-bundle.crt="${TMP_CA}" \
      --dry-run=client -o yaml | oc apply -f -

    oc patch proxy/cluster --type=merge \
      -p '{"spec":{"trustedCA":{"name":"user-ca-bundle"}}}'

    echo "==> [pre] Restarting patterns-operator (picks up cluster CA trust for git clone)"
    if oc get deployment patterns-operator-controller-manager -n "${PATTERNS_NS}" >/dev/null 2>&1; then
      oc rollout restart deployment/patterns-operator-controller-manager -n "${PATTERNS_NS}"
      oc rollout status deployment/patterns-operator-controller-manager -n "${PATTERNS_NS}"
    else
      echo "    warning: deployment/patterns-operator-controller-manager not found in ${PATTERNS_NS}" >&2
    fi
  else
    echo "==> [pre] No clusterProxy CAs configured; skipping Proxy trust"
  fi
fi

if (( RUN_POST )); then
  if (( HAS_ARGOCD_TLS == 0 )) && ((${#SECRET_FILES[@]} == 0)); then
    echo "==> [post] No Argo CD TLS or repo-creds configured; skipping"
  else
    # Wait for GitOps namespace (created during pattern install).
    wait_for_ns "${ARGOCD_NS}"

    # Apply repo-creds as soon as the namespace exists so OCI pulls can auth
    # even while waiting for Argo CD TLS ConfigMaps.
    if ((${#SECRET_FILES[@]} > 0)); then
      echo "==> [post] Applying Argo CD registry repo-creds Secrets"
      for sf in "${SECRET_FILES[@]}"; do
        oc apply -f "${HERE}/secrets/${sf}"
      done
    fi

    if (( HAS_ARGOCD_TLS == 1 )); then
      wait_for_cm "${ARGOCD_NS}" "argocd-tls-certs-cm"
      echo "==> [post] Patching Argo CD TLS trust in ${ARGOCD_NS}"
      oc patch configmap argocd-tls-certs-cm -n "${ARGOCD_NS}" --type=merge --patch-file="${HERE}/argocd-tls-certs-patch.yaml"

      TMP_BUNDLE="$(mktemp)"
      {
        oc get configmap trusted-ca-bundle -n "${ARGOCD_NS}" -o jsonpath='{.data.ca-bundle\.crt}' 2>/dev/null || true
        echo
        cat "${HERE}/trusted-ca-bundle-append.pem"
      } > "${TMP_BUNDLE}"
      if oc get configmap trusted-ca-bundle -n "${ARGOCD_NS}" >/dev/null 2>&1; then
        {
          echo "data:"
          echo "  ca-bundle.crt: |"
          sed 's/^/    /' "${TMP_BUNDLE}"
        } > "${TMP_BUNDLE}.patch"
        oc patch configmap trusted-ca-bundle -n "${ARGOCD_NS}" --type=merge --patch-file="${TMP_BUNDLE}.patch"
        rm -f "${TMP_BUNDLE}.patch"
      else
        oc create configmap trusted-ca-bundle -n "${ARGOCD_NS}" --from-file=ca-bundle.crt="${TMP_BUNDLE}"
      fi
      rm -f "${TMP_BUNDLE}"
    fi

    if wait_for_deploy "${ARGOCD_NS}" "${REPO_SERVER}"; then
      echo "==> [post] Restarting deployment/${REPO_SERVER}"
      oc rollout restart "deployment/${REPO_SERVER}" -n "${ARGOCD_NS}"
      oc rollout status "deployment/${REPO_SERVER}" -n "${ARGOCD_NS}" --timeout="${WAIT_SECONDS}s" || \
        oc rollout status "deployment/${REPO_SERVER}" -n "${ARGOCD_NS}"
    else
      echo "    warning: skipping repo-server restart; apply secrets/TLS anyway (restart later if needed)" >&2
    fi
  fi
fi

echo "Done (phase=${PHASE})."
echo "  Restore original Proxy with: ${HERE}/restore-proxy-cluster.sh"
`

	tmpl, err := template.New("apply").Parse(script)
	if err != nil {
		return err
	}
	path := filepath.Join(outDir, "apply-airgap.sh")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.Execute(f, d)
}

func writeRestoreProxyScript(outDir string) error {
	const script = `#!/usr/bin/env bash
# Generated by patternizer airgap bootstrap
# Restores proxy/cluster (and user-ca-bundle when backed up) from first-time backups.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BACKUP_DIR="${HERE}/backup"
PROXY_BACKUP="${BACKUP_DIR}/proxy-cluster.yaml"
USER_CA_BACKUP="${BACKUP_DIR}/user-ca-bundle.yaml"

if ! command -v oc >/dev/null 2>&1; then
  echo "oc is required but not found on PATH" >&2
  exit 1
fi
if [[ ! -f "${PROXY_BACKUP}" ]]; then
  echo "missing ${PROXY_BACKUP}; run apply-airgap.sh first" >&2
  exit 1
fi

echo "==> Restoring proxy/cluster from ${PROXY_BACKUP}"
# Drop cluster-scoped metadata that blocks apply, keep spec.
oc apply -f "${PROXY_BACKUP}"

if [[ -f "${USER_CA_BACKUP}" ]]; then
  echo "==> Restoring ConfigMap user-ca-bundle from ${USER_CA_BACKUP}"
  oc apply -f "${USER_CA_BACKUP}"
else
  echo "==> No user-ca-bundle backup; clearing Proxy trustedCA name only if desired manually"
fi

echo "Done. You may need to restart patterns-operator-controller-manager after restore."
`
	path := filepath.Join(outDir, "restore-proxy-cluster.sh")
	return os.WriteFile(path, []byte(script), 0o755)
}

func writeBootstrapReadme(outDir, secretsPath, ns, poNS string, certs []ResolvedCertificate, regs []ResolvedRegistry, result *BootstrapResult) error {
	var b strings.Builder
	b.WriteString("# Airgap bootstrap (generated by patternizer airgap bootstrap)\n\n")
	b.WriteString("Single apply for cluster Proxy CA trust (patterns-operator git clone),\n")
	b.WriteString("Argo CD TLS trust, and registry repo-creds.\n\n")
	fmt.Fprintf(&b, "Source secrets file: `%s`\n\n", secretsPath)
	if result != nil && len(result.RenderedFiles) > 0 {
		b.WriteString("## Rendered pattern values\n\n")
		fmt.Fprintf(&b, "- helmRepoUrl: `%s`\n", result.HelmRepoURL)
		fmt.Fprintf(&b, "- patternsOperator.source: `%s`\n", result.CommunityCS)
		fmt.Fprintf(&b, "- gitops.operatorSource: `%s`\n", result.RedHatCS)
		b.WriteString("- Updated files (backups under `backup/values/`):\n")
		for _, f := range result.RenderedFiles {
			fmt.Fprintf(&b, "  - `%s`\n", f)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Apply once\n\n")
	b.WriteString("```bash\n./apply-airgap.sh          # all (default)\n./apply-airgap.sh pre      # Proxy CA only (before make install)\n./apply-airgap.sh post     # Argo CD TLS + repo-creds (after vp-gitops)\n```\n\n")
	b.WriteString("Or from the pattern repo:\n\n")
	b.WriteString("```bash\n./pattern.sh make install-airgap\n```\n\n")
	b.WriteString("This script:\n\n")
	b.WriteString("1. Backs up `proxy/cluster` to `backup/proxy-cluster.yaml` (first run only, pre phase)\n")
	b.WriteString("2. Merges CAs into `openshift-config/user-ca-bundle` and sets `Proxy.spec.trustedCA`\n")
	b.WriteString("3. Restarts patterns-operator\n")
	b.WriteString("4. Patches Argo CD TLS ConfigMaps (when configured, post phase)\n")
	b.WriteString("5. Applies registry repo-creds Secrets (when configured, post phase)\n")
	b.WriteString("6. Restarts the Argo CD repo-server\n\n")
	b.WriteString("## Restore Proxy\n\n")
	b.WriteString("```bash\n./restore-proxy-cluster.sh\n```\n\n")
	fmt.Fprintf(&b, "Namespaces: Argo CD `%s`, patterns-operator `%s`\n\n", ns, poNS)
	if len(certs) > 0 {
		b.WriteString("## Certificates\n\n")
		for _, c := range certs {
			fmt.Fprintf(&b, "- `%s` host `%s` clusterProxy=%v argocdTLS=%v\n", c.Name, c.Host, c.ClusterProxy, c.ArgocdTLS)
		}
		b.WriteString("\n")
	}
	if len(regs) > 0 {
		b.WriteString("## Registries\n\n")
		for _, r := range regs {
			fmt.Fprintf(&b, "- `%s` url `%s`\n", r.Name, r.URL)
		}
		b.WriteString("\nDo not commit `secrets/*.yaml` — they contain passwords.\n")
	}
	return os.WriteFile(filepath.Join(outDir, "README.md"), []byte(b.String()), 0o644)
}

func sanitizeFileName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "item"
	}
	return out
}
