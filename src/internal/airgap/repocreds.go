package airgap

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// RepoCredsOptions configures generate-only Argo CD registry repo-creds artifacts.
type RepoCredsOptions struct {
	DockerConfig string
	Username     string
	Password     string
	PasswordFile string
	URL          string
	Namespace    string
	OutputDir    string
	SecretName   string
	RepoServer   string
}

// RepoCredsResult summarizes generated repo-creds artifacts.
type RepoCredsResult struct {
	OutputDir  string
	Namespace  string
	SecretName string
	URL        string
	Username   string
	RepoServer string
}

// RegistryCredential is a resolved registry username/password for a URL prefix.
type RegistryCredential struct {
	URL      string
	Username string
	Password string
}

// GenerateRepoCreds writes an Argo CD repo-creds Secret and apply script.
// Credentials come from --docker-config or --username/--password (never hardcoded).
func GenerateRepoCreds(opts *RepoCredsOptions) (*RepoCredsResult, error) {
	if opts == nil {
		return nil, fmt.Errorf("options are required")
	}

	cred, err := ResolveRegistryCredential(opts)
	if err != nil {
		return nil, err
	}

	ns := strings.TrimSpace(opts.Namespace)
	if ns == "" {
		ns = "vp-gitops"
	}
	outDir := strings.TrimSpace(opts.OutputDir)
	if outDir == "" {
		outDir = "./airgap-repo-creds"
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return nil, err
	}
	outDir = absOut

	secretName := strings.TrimSpace(opts.SecretName)
	if secretName == "" {
		secretName = defaultRepoCredsSecretName(cred.URL)
	}
	repoServer := strings.TrimSpace(opts.RepoServer)
	if repoServer == "" {
		repoServer = ns + "-repo-server"
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	secretPath := filepath.Join(outDir, "argocd-repo-creds-secret.yaml")
	if err := writeRepoCredsSecret(secretPath, ns, secretName, cred); err != nil {
		return nil, fmt.Errorf("write argocd-repo-creds-secret.yaml: %w", err)
	}
	if err := writeApplyRepoCredsScript(outDir, ns, secretName, repoServer); err != nil {
		return nil, fmt.Errorf("write apply-repo-creds.sh: %w", err)
	}
	if err := writeRepoCredsReadme(outDir, ns, secretName, repoServer, cred.URL, cred.Username); err != nil {
		return nil, fmt.Errorf("write README.md: %w", err)
	}

	return &RepoCredsResult{
		OutputDir:  outDir,
		Namespace:  ns,
		SecretName: secretName,
		URL:        cred.URL,
		Username:   cred.Username,
		RepoServer: repoServer,
	}, nil
}

// ResolveRegistryCredential loads credentials from docker config or explicit flags.
func ResolveRegistryCredential(opts *RepoCredsOptions) (*RegistryCredential, error) {
	hasDocker := strings.TrimSpace(opts.DockerConfig) != ""
	hasUser := strings.TrimSpace(opts.Username) != ""
	hasPass := strings.TrimSpace(opts.Password) != "" || strings.TrimSpace(opts.PasswordFile) != ""

	if hasDocker && (hasUser || hasPass) {
		return nil, fmt.Errorf("use either --docker-config or --username/--password, not both")
	}
	if !hasDocker && !hasUser {
		return nil, fmt.Errorf("provide --docker-config or --username (with --password / --password-file)")
	}

	if hasDocker {
		return credentialFromDockerConfig(opts.DockerConfig, opts.URL)
	}

	password, err := loadPassword(opts.Password, opts.PasswordFile)
	if err != nil {
		return nil, err
	}
	if password == "" {
		return nil, fmt.Errorf("--password or --password-file is required with --username")
	}
	regURL := NormalizeRegistryURL(opts.URL)
	if regURL == "" {
		return nil, fmt.Errorf("--url is required with --username/--password (e.g. mirror.example.com:8443)")
	}
	return &RegistryCredential{
		URL:      regURL,
		Username: strings.TrimSpace(opts.Username),
		Password: password,
	}, nil
}

func loadPassword(password, passwordFile string) (string, error) {
	if strings.TrimSpace(passwordFile) != "" {
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			return "", fmt.Errorf("read --password-file: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	return password, nil
}

func credentialFromDockerConfig(path, wantURL string) (*RegistryCredential, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --docker-config %s: %w", path, err)
	}
	var cfg struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse docker config JSON: %w", err)
	}
	if len(cfg.Auths) == 0 {
		return nil, fmt.Errorf("docker config %s has no auths entries", path)
	}

	want := NormalizeRegistryURL(wantURL)
	type entry struct {
		key        string
		user, pass string
	}
	var entries []entry
	for key, auth := range cfg.Auths {
		user, pass, err := decodeDockerAuth(auth.Auth, auth.Username, auth.Password)
		if err != nil {
			return nil, fmt.Errorf("auth entry %q: %w", key, err)
		}
		if user == "" && pass == "" {
			continue
		}
		entries = append(entries, entry{key: key, user: user, pass: pass})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("docker config %s has no usable username/password auths", path)
	}

	var matched *entry
	if want != "" {
		for i := range entries {
			e := &entries[i]
			norm := NormalizeRegistryURL(e.key)
			if norm == want || strings.HasPrefix(want, norm) || strings.HasPrefix(norm, want) {
				matched = e
				break
			}
			// Match host without port against host:port keys and vice versa.
			if registryHost(norm) == registryHost(want) {
				matched = e
				break
			}
		}
		if matched == nil {
			keys := make([]string, 0, len(entries))
			for _, e := range entries {
				keys = append(keys, e.key)
			}
			return nil, fmt.Errorf("no docker config auth for %q (have: %s)", want, strings.Join(keys, ", "))
		}
	} else {
		if len(entries) != 1 {
			keys := make([]string, 0, len(entries))
			for _, e := range entries {
				keys = append(keys, e.key)
			}
			return nil, fmt.Errorf("docker config has %d auths; pass --url to select one (have: %s)", len(entries), strings.Join(keys, ", "))
		}
		matched = &entries[0]
	}

	regURL := want
	if regURL == "" {
		regURL = NormalizeRegistryURL(matched.key)
	}
	return &RegistryCredential{
		URL:      regURL,
		Username: matched.user,
		Password: matched.pass,
	}, nil
}

func decodeDockerAuth(authB64, username, password string) (user, pass string, err error) {
	if username != "" || password != "" {
		return username, password, nil
	}
	authB64 = strings.TrimSpace(authB64)
	if authB64 == "" {
		return "", "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(authB64)
	if err != nil {
		// Some configs use URL-safe or missing padding.
		raw, err = base64.RawStdEncoding.DecodeString(authB64)
		if err != nil {
			return "", "", fmt.Errorf("decode auth field: %w", err)
		}
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("auth field is not username:password")
	}
	return parts[0], parts[1], nil
}

// NormalizeRegistryURL returns host[:port][/path] suitable for Argo CD repo-creds url.
// Keeps the port (unlike TLS cert map keys). Strips scheme only.
func NormalizeRegistryURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	candidate := raw
	if strings.Contains(candidate, "://") {
		u, err := url.Parse(candidate)
		if err != nil {
			return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://"), "/")
		}
		candidate = u.Host
		if u.Path != "" && u.Path != "/" {
			candidate += strings.TrimSuffix(u.Path, "/")
		}
	}
	return strings.TrimSuffix(candidate, "/")
}

func registryHost(regURL string) string {
	regURL = NormalizeRegistryURL(regURL)
	if i := strings.Index(regURL, "/"); i >= 0 {
		regURL = regURL[:i]
	}
	if strings.HasPrefix(regURL, "[") {
		if end := strings.Index(regURL, "]"); end >= 0 {
			return regURL[:end+1]
		}
	}
	if i := strings.LastIndex(regURL, ":"); i >= 0 {
		return regURL[:i]
	}
	return regURL
}

var secretNameSanitizer = regexp.MustCompile(`[^a-z0-9.-]+`)

func defaultRepoCredsSecretName(regURL string) string {
	host := registryHost(regURL)
	host = strings.ToLower(host)
	host = secretNameSanitizer.ReplaceAllString(host, "-")
	host = strings.Trim(host, "-.")
	if host == "" {
		host = "registry"
	}
	name := host + "-repo-creds"
	if len(name) > 63 {
		name = name[:63]
		name = strings.Trim(name, "-.")
	}
	return name
}

func writeRepoCredsSecret(path, namespace, secretName string, cred *RegistryCredential) error {
	// Escape YAML double-quoted strings for password/username that may contain special chars.
	var b strings.Builder
	b.WriteString("# Generated by patternizer airgap repo-creds\n")
	b.WriteString("# Contains registry credentials — do not commit to Git.\n")
	b.WriteString("apiVersion: v1\n")
	b.WriteString("kind: Secret\n")
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", secretName)
	fmt.Fprintf(&b, "  namespace: %s\n", namespace)
	b.WriteString("  labels:\n")
	b.WriteString("    argocd.argoproj.io/secret-type: repo-creds\n")
	b.WriteString("stringData:\n")
	fmt.Fprintf(&b, "  url: %s\n", yamlDoubleQuoted(cred.URL))
	fmt.Fprintf(&b, "  username: %s\n", yamlDoubleQuoted(cred.Username))
	fmt.Fprintf(&b, "  password: %s\n", yamlDoubleQuoted(cred.Password))
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func yamlDoubleQuoted(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	return `"` + escaped + `"`
}

func writeApplyRepoCredsScript(outDir, namespace, secretName, repoServer string) error {
	const script = `#!/usr/bin/env bash
# Generated by patternizer airgap repo-creds
# Applies Argo CD registry repo-creds Secret {{.SecretName}} in {{.Namespace}}
# Requires: oc on PATH and a working kubeconfig (KUBECONFIG or ~/.kube/config)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
NAMESPACE="{{.Namespace}}"
SECRET_NAME="{{.SecretName}}"
REPO_SERVER="{{.RepoServer}}"
SECRET_FILE="${HERE}/argocd-repo-creds-secret.yaml"

if ! command -v oc >/dev/null 2>&1; then
  echo "oc is required but not found on PATH" >&2
  exit 1
fi
if [[ ! -f "${SECRET_FILE}" ]]; then
  echo "missing ${SECRET_FILE}" >&2
  exit 1
fi

echo "==> Checking namespace ${NAMESPACE}"
oc get namespace "${NAMESPACE}" >/dev/null

echo "==> Applying Secret ${SECRET_NAME}"
oc apply -f "${SECRET_FILE}"

echo "==> Restarting deployment/${REPO_SERVER}"
oc rollout restart "deployment/${REPO_SERVER}" -n "${NAMESPACE}"
oc rollout status "deployment/${REPO_SERVER}" -n "${NAMESPACE}"

echo "Done. Verify with:"
echo "  oc get secret ${SECRET_NAME} -n ${NAMESPACE} --show-labels"
echo "  oc get application -n ${NAMESPACE}"
`

	tmpl, err := template.New("apply").Parse(script)
	if err != nil {
		return err
	}
	path := filepath.Join(outDir, "apply-repo-creds.sh")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, map[string]string{
		"Namespace":  namespace,
		"SecretName": secretName,
		"RepoServer": repoServer,
	})
}

func writeRepoCredsReadme(outDir, namespace, secretName, repoServer, regURL, username string) error {
	var b strings.Builder
	b.WriteString("# Argo CD registry repo-creds (generated by patternizer airgap repo-creds)\n\n")
	b.WriteString("Creates an Argo CD `repo-creds` Secret so the repo-server can authenticate to a\n")
	b.WriteString("private OCI/Helm registry (Quay mirror, Harbor, etc.).\n\n")
	b.WriteString("**Do not commit** `argocd-repo-creds-secret.yaml` to Git — it contains a password.\n\n")
	b.WriteString("## Prerequisites\n\n")
	b.WriteString("- `oc` on PATH\n")
	b.WriteString("- Working kubeconfig (`KUBECONFIG` or `~/.kube/config`)\n")
	fmt.Fprintf(&b, "- Namespace `%s` with Argo CD installed\n", namespace)
	fmt.Fprintf(&b, "- Deployment `%s`\n\n", repoServer)
	b.WriteString("## Registry\n\n")
	fmt.Fprintf(&b, "- URL: `%s`\n", regURL)
	fmt.Fprintf(&b, "- Username: `%s`\n", username)
	fmt.Fprintf(&b, "- Secret: `%s`\n\n", secretName)
	b.WriteString("## Apply\n\n")
	b.WriteString("```bash\n")
	b.WriteString("./apply-repo-creds.sh\n")
	b.WriteString("```\n\n")
	b.WriteString("Typically run **after** `airgap trust-ca` for the same registry hostname so TLS\n")
	b.WriteString("and auth are both configured.\n\n")
	b.WriteString("## Confirm\n\n")
	b.WriteString("```bash\n")
	fmt.Fprintf(&b, "oc get secret %s -n %s --show-labels\n", secretName, namespace)
	fmt.Fprintf(&b, "oc get application -n %s\n", namespace)
	b.WriteString("# Sync errors should no longer show 401 unauthorized for the mirror\n")
	b.WriteString("```\n")
	return os.WriteFile(filepath.Join(outDir, "README.md"), []byte(b.String()), 0o644)
}
