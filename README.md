# Patternizer

![Version: 2.12.1](https://img.shields.io/badge/Version-2.12.1-informational?style=flat-square)
[![Quay Repository](https://img.shields.io/badge/Quay.io-patternizer-blue?logo=quay)](https://quay.io/repository/validatedpatterns/patternizer)
[![CI Pipeline](https://github.com/validatedpatterns/patternizer/actions/workflows/build-push.yaml/badge.svg?branch=main)](https://github.com/validatedpatterns/patternizer/actions/workflows/build-push.yaml)

**Patternizer** is a command-line tool that bootstraps a Git repository containing Helm charts into a ready-to-use Validated Pattern. It automatically generates the necessary scaffolding, configuration files, and utility scripts, so you can get your pattern up and running in minutes. It can also be used to upgrade existing patterns as described in [the Validated Pattern's blog](https://validatedpatterns.io/blog/2025-08-29-new-common-makefile-structure/).

**Note:** This repo was developed with AI tools including [Cursor](https://cursor.com/), [Claude](https://claude.ai/login) and [Gemini](https://gemini.google.com/app).

- [Patternizer](#patternizer)
  - [Quick Start](#quick-start)
  - [Example Workflow](#example-workflow)
  - [Usage Details](#usage-details)
    - [Container Usage (Recommended)](#container-usage-recommended)
      - [**Initialize without secrets:**](#initialize-without-secrets)
      - [**Initialize with secrets support:**](#initialize-with-secrets-support)
      - [**Upgrade an existing pattern repository:**](#upgrade-an-existing-pattern-repository)
      - [**Shell alias (optional):**](#shell-alias-optional)
    - [Understanding Secrets Management](#understanding-secrets-management)
    - [Generated Files](#generated-files)
    - [AI Coding Skills](#ai-coding-skills)
  - [Development \& Contributing](#development--contributing)
    - [Prerequisites](#prerequisites)
    - [Local Development Workflow](#local-development-workflow)
    - [How to Contribute](#how-to-contribute)

## Quick Start

**Prerequisites:**

- Podman or Docker
- A Git repository you want to convert into a pattern

Navigate to your repository's root directory and run the initialization command:

```bash
podman run --pull=newer -v "$PWD:$PWD:z" -w "$PWD" quay.io/validatedpatterns/patternizer init
```

This single command will generate all the necessary files to turn your repository into a Validated Pattern.

## Example Workflow

1.  **Clone or create your pattern repository**

    ```bash
    git clone https://github.com/your-org/your-pattern.git
    cd your-pattern
    git checkout -b initialize-pattern
    ```

2.  **Initialize the pattern using Patternizer**

    ```bash
    podman run --pull=newer -v "$PWD:$PWD:z" -w "$PWD" quay.io/validatedpatterns/patternizer init
    ```

3.  **Review, commit, and push the generated files**

    ```bash
    git status
    git add .
    git commit -m 'initialize pattern using patternizer'
    git push -u origin initialize-pattern
    ```

4. **Login to an OpenShift cluster**

    ```bash
    export KUBECONFIG=/path/to/cluster/kubeconfig
    ```

5.  **Install the pattern**

    ```bash
    ./pattern.sh make install
    ```

## Usage Details

### Container Usage (Recommended)

Using the prebuilt container is the easiest way to run Patternizer, as it requires no local installation.

#### **Initialize without secrets:**

```bash
podman run --pull=newer -v "$PWD:$PWD:z" -w "$PWD" quay.io/validatedpatterns/patternizer init
```

#### **Initialize with secrets support:**

```bash
podman run --pull=newer -v "$PWD:$PWD:z" -w "$PWD" quay.io/validatedpatterns/patternizer init --with-secrets
```

#### **Upgrade an existing pattern repository:**

Use this to migrate or refresh an existing pattern repo to the latest common structure and scripts.

```bash
# Refresh common assets, keep your Makefile unless it lacks the include
podman run --pull=newer -v "$PWD:$PWD:z" -w "$PWD" quay.io/validatedpatterns/patternizer upgrade

# Replace your Makefile with the default from Patternizer
podman run --pull=newer -v "$PWD:$PWD:z" -w "$PWD" quay.io/validatedpatterns/patternizer upgrade --replace-makefile
```

#### **Shell alias (optional):**

You can shorten the patternizer command by adding a shell function to your shell's startup file (e.g. `~/.bashrc` or `~/.zshrc`):

```bash
pattern() {
  podman run --pull=newer \
    -v "$PWD:$PWD:z" \
    -w "$PWD" \
    quay.io/validatedpatterns/patternizer "$@"
}
```

Then run commands as `pattern init`, `pattern upgrade`, `pattern airgap init`, etc.

#### **Airgap / disconnected init:**

Generate an oc-mirror v2 `imagesetconfig.yaml` plus disconnected install helpers from a local absolute path or a Git URL.

Path-based Helm charts referenced by pattern applications (`path: charts/...`)
are scanned for container images (via `helm template` and values scrape); those
images are emitted under `additionalImages` only (the charts are not added to
`helm.local`). Remote OCI charts (`chart:` / `chartVersion:`) are resolved and
scanned as well. Use `--extra-config` for extra operators, images, and charts
not declared in values:

```yaml
# ~/extra-images.yaml
operators:
  - catalog: registry.redhat.io/redhat/redhat-operator-index:v4.22
    packages:
      - name: lvms-operator
additionalImages:
  - name: registry.example.com/app:1.0
helm:
  local:
    - name: my-chart
      path: /abs/path/to/chart
```

```bash
# Absolute path to a local pattern checkout
podman run --pull=newer -v "$PWD:$PWD:z" -v "$HOME:$HOME:z" -w "$PWD" quay.io/validatedpatterns/patternizer \
  airgap init /path/to/multicloud-gitops \
  --extra-config "$HOME/extra-images.yaml" \
  --output-dir "$PWD/airgap-out"

# Include OpenShift platform channel (stable-4.19) in imagesetconfig.yaml
podman run --pull=newer -v "$PWD:$PWD:z" -v "$HOME:$HOME:z" -w "$PWD" quay.io/validatedpatterns/patternizer \
  airgap init /path/to/multicloud-gitops \
  --ocp-version 4.19 \
  --extra-config "$HOME/extra-images.yaml" \
  --output-dir "$PWD/airgap-out"

# Or clone from Git into --output-dir (override with --clone-dir)
podman run --pull=newer -v "$PWD:$PWD:z" -w "$PWD" quay.io/validatedpatterns/patternizer \
  airgap init https://github.com/validatedpatterns/multicloud-gitops \
  --git-ref main \
  --download-oc-mirror \
  --output-dir "$PWD/airgap-out"
```

`--registry` is optional (sets `helmRepoUrl` in `values-disconnected.yaml` and the
registry placeholder in `mirror-commands.sh`). `--ocp-version` is optional and only
adds `mirror.platform` with channel `stable-<version>`.

Git clones land under `--output-dir/<repo-name>/`. Run `oc-mirror` yourself using
`mirror-commands.sh` (patternizer only generates the config and helpers).

Or from a pattern repository after `Makefile-common` includes the target:

```bash
./pattern.sh make airgap-init \
  AIRGAP_EXTRA_OPTS="--extra-config $HOME/extra-images.yaml"
# Optional: AIRGAP_OCP_VERSION=4.19 AIRGAP_REGISTRY=mirror.example.com:8443
```

Outputs under `--output-dir`:

- `imagesetconfig.yaml`
- `values-disconnected.yaml`
- `mirror-commands.sh`
- `CHECKLIST.md` (pull secrets, CatalogSources, Git, Vault/ESO)
- cloned pattern repo (when source is a Git URL)

Parse oc-mirror logs for skipped `oci://` related images:

```bash
pattern airgap parse-mirror-log /path/to/oc-mirror.log
```

#### **Airgap trust CA (Argo CD / GitOps):**

Generate apply-ready artifacts so OpenShift GitOps trusts a private or self-signed
CA for HTTPS Git hosts (e.g. Gitea) or OCI registries (e.g. Quay mirror). Patternizer
only writes files; you apply them on a bastion with `oc` and your kubeconfig.

```bash
# Gitea (or any Git host) CA
podman run --pull=newer -v "$PWD:$PWD:z" -v /home/fedora/gitea_container_certs:/certs:z \
  -w "$PWD" quay.io/validatedpatterns/patternizer \
  airgap trust-ca \
  --ca /certs/ca.crt \
  --host gitea-gitea.apps.airgap.rhuk.lab \
  --namespace vp-gitops \
  --output-dir "$PWD/airgap-trust"

# Mirror registry CA (hostname only; no port)
patternizer airgap trust-ca \
  --ca /etc/pki/ca-trust/source/anchors/quay-rootCA.pem \
  --host mirror.airgap.rhuk.lab \
  --output-dir ./airgap-trust
```

`--ca` may be a PEM file or a directory of `.crt`/`.pem` files. `--host` is
repeatable (or comma-separated); URLs and `:port` suffixes are normalized to
hostname keys for `argocd-tls-certs-cm`.

Outputs under `--output-dir`:

- `argocd-tls-certs-patch.yaml`
- `trusted-ca-bundle-append.pem`
- `apply-trust-ca.sh` (run on the bastion)
- `README.md`

```bash
# On the bastion (cluster reachable via ~/.kube/config or KUBECONFIG):
./airgap-trust/apply-trust-ca.sh
```

#### **Airgap registry repo-creds (Argo CD auth):**

After TLS trust is in place, Argo CD still needs credentials to pull OCI charts from a
private mirror (otherwise sync fails with `401 unauthorized`). Generate a
`repo-creds` Secret from a docker config **or** username/password — credentials are
never hardcoded in patternizer:

```bash
# From ~/.docker/config.json (or podman containers auth.json)
patternizer airgap repo-creds \
  --docker-config ~/.docker/config.json \
  --url mirror.airgap.rhuk.lab:8443 \
  --namespace vp-gitops \
  --output-dir ./airgap-repo-creds

# Or explicit credentials (prefer --password-file)
patternizer airgap repo-creds \
  --url mirror.example.com:8443 \
  --username init \
  --password-file /path/to/registry.password \
  --output-dir ./airgap-repo-creds
```

`--url` keeps the port (e.g. `:8443`) for Argo CD. With multiple `auths` in the docker
config, `--url` selects which registry entry to use.

Outputs under `--output-dir`:

- `argocd-repo-creds-secret.yaml` (mode `0600` — **do not commit**)
- `apply-repo-creds.sh`
- `README.md`

```bash
./airgap-repo-creds/apply-repo-creds.sh
```

#### **Airgap bootstrap (recommended one-shot):**

Configure hostnames, CA files, and registry credentials in the pattern’s
`values-secret.yaml` / `~/values-secret-<pattern>.yaml` under a top-level `airgap:`
section (same `path` / `value` field style as Vault secrets; **not** loaded into Vault).

Then install with the airgap-aware target (leaves `make install` unchanged for connected clusters):

```bash
# Edit airgap: in ~/values-secret-multicloud-gitops.yaml (see values-secret.yaml.template)
./pattern.sh make install-airgap
```

That runs:

1. `airgap-trust-pre` — Proxy CA / `user-ca-bundle`  
2. **`make install` in the background** (log: `airgap-bootstrap/install.log`)  
3. `airgap-wait-gitops` — waits for `vp-gitops` while install is still running  
4. `airgap-trust-post` — repo-creds + TLS (unblocks Argo CD / install)  
5. Waits for the background `make install` to finish  

This avoids the deadlock where install never completes until post trust is applied.
Default wait is 20 minutes (`AIRGAP_WAIT_SECONDS=1200`):

```bash
./pattern.sh make install-airgap AIRGAP_WAIT_SECONDS=1800
```

Declare CatalogSources in `~/values-secret-<pattern>.yaml` under `airgap:`:

```yaml
airgap:
  registry: mirror.airgap.rhuk.lab:8443
  ocpVersion: "4.22"   # derives cs-*-operator-index-v4-22
  # Or:
  # catalogSources:
  #   redhat: cs-redhat-operator-index-v4-22
  #   community: cs-community-operator-index-v4-22
  # Or: discoverCatalogs: true   # uses oc get catalogsource
```

Manual generate:

```bash
patternizer airgap bootstrap /home/fedora/multicloud-gitops \
  --output-dir ./airgap-bootstrap
# or: --discover-catalogs   # requires oc/kubeconfig
```

Proxy backup is written to `./airgap-bootstrap/backup/proxy-cluster.yaml`. Restore with:

```bash
./pattern.sh make airgap-restore-proxy
```

Overrides: `AIRGAP_BOOTSTRAP_DIR`, `AIRGAP_SECRETS_FILE`, `AIRGAP_PATTERNIZER_BIN`,
`AIRGAP_PATTERNIZER_IMAGE`, `AIRGAP_WAIT_SECONDS`.

Lower-level commands `airgap trust-ca` and `airgap repo-creds` remain available
for one-off use without a values-secret file.

See the [disconnected installation guide](https://validatedpatterns.io/learn/disconnected-installation/).

What upgrade does:

- Removes the `common/` directory if it exists
- Updates `ansible.cfg`, `Makefile-common`, and `pattern.sh` to the latest versions from [the resources directory](./resources/)
- Makefile handling:
  - If `--replace-makefile` is set: replaces an existing Makefile, if present, to [`Makefile`](./resources/Makefile) from the resources directory
  - If not set:
    - If no `Makefile` exists: copies the default `Makefile`
    - If a `Makefile` exists and already contains `include Makefile-common` anywhere: leaves it unchanged
    - Otherwise: prepends `include Makefile-common` to the first line so your existing targets are preserved
- Prints a deprecation warning if `values-global.yaml` still uses `main.clusterGroupName`, which the Validated Patterns operator deprecated in favor of `main.variant` in version 0.0.78 (`upgrade` does not modify the file; run `patternizer init` to migrate automatically)
- Prints a deprecation warning if the pattern does not use the [variants folder structure](https://validatedpatterns.io/blog/2026-08-04-variants-folder-structure/) (introduced in operator version 0.0.78) — that is, if `variants/<variant>/values-<variant>.yaml` is missing for the configured variant (`upgrade` does not move any files; run `patternizer init` to migrate automatically)
- Prints a warning if `global.singleArgoCD` is not set in `values-global.yaml`, recommending you set `global.singleArgoCD: true` if possible (see the [single ArgoCD instance](https://validatedpatterns.io/blog/2026-06-04-single-argocd-option/) blog post)

These warnings are written to stderr, grouped after the success message, so they stand out as a call to action.

### Understanding Secrets Management

You can start simple and add secrets management later.

- By default, `patternizer init` disables secret loading.
- To add secrets scaffolding, run `patternizer init --with-secrets` at any time. This will update your configuration to enable secrets.
- **Important:** This action is not easily reversible. We recommend committing your work to Git _before_ adding secrets support.

For more details on how secrets work in the framework, see the [Secrets Management Documentation](https://validatedpatterns.io/learn/secrets-management-in-the-validated-patterns-framework/).

### Generated Files

Running `patternizer init` creates the following:

- `values-global.yaml`: Global pattern configuration.
- `variants/<variant>/values-<variant>.yaml`: Variant-specific (cluster group) values, using the [variants folder structure](https://validatedpatterns.io/blog/2026-08-04-variants-folder-structure/). A legacy root-level `values-<variant>.yaml` is migrated into this folder automatically.
- `pattern.sh`: A utility script for common pattern operations (`install`, `upgrade`, etc.).
- `Makefile`: A simple Makefile that includes `Makefile-common`.
- `Makefile-common`: The core Makefile with all pattern-related targets.
- `ansible.cfg`: Configuration for the ansible installation used when `./pattern.sh` is called
- `.claude/skills/pattern-author/`: AI coding skill for Claude Code (see [AI Coding Skills](#ai-coding-skills))
- `.cursor/skills/pattern-author/`: AI coding skill for Cursor (see [AI Coding Skills](#ai-coding-skills))

Using the `--with-secrets` flag additionally creates:

- `values-secret.yaml.template`: A template for defining your secrets.
- It also updates `values-global.yaml` to set `global.secretLoader.disabled: false` and adds Vault and External Secrets Operator to the cluster group values.

### AI Coding Skills

Both `init` and `upgrade` install the **pattern-author** skill into your pattern repository. This skill teaches AI coding assistants how to work with Validated Patterns — including the values file structure, operator subscriptions, secrets framework, and hub/spoke configuration.

The skill is installed for both [Claude Code](https://docs.anthropic.com/en/docs/claude-code) and [Cursor](https://cursor.com/) using the [Agent Skills](https://cursor.com/docs/skills) open standard. The same skill files work in both tools. Any existing skills or configuration you have in your `.claude/` or `.cursor/` directories are left untouched.

To use the skill, open your pattern repository in Claude Code or Cursor and ask the assistant to help with pattern authoring tasks — for example, adding an operator, configuring secrets, or setting up a spoke cluster.

## Development & Contributing

This section is for developers who want to contribute to the Patternizer project itself.

### Prerequisites

- Go (see [`go.mod`](./src/go.mod) for version)
- Podman or Docker
- Git
- Make

### Local Development Workflow

```bash
# 1. Clone the repository
git clone https://github.com/validatedpatterns/patternizer.git
cd patternizer

# 2. Set up the development environment
make dev-setup

# 3. Make your changes...

# 4. Run the full CI suite locally before committing
make ci
```

### How to Contribute

1.  Fork the repository.
2.  Create a feature branch for your changes.
3.  Make your changes and ensure they pass the local CI check (`make ci`).
4.  Submit a pull request.
