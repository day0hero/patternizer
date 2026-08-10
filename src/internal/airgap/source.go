package airgap

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PatternSource is a resolved local pattern directory.
type PatternSource struct {
	Dir     string
	FromGit bool
	GitURL  string
}

// ResolvePatternSource resolves an absolute path or Git URL to a local pattern directory.
func ResolvePatternSource(input string, opts *Options) (*PatternSource, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("pattern source is required: provide an absolute path or a Git repository URL")
	}

	if IsGitURL(input) {
		return clonePattern(input, opts)
	}

	if !filepath.IsAbs(input) {
		return nil, fmt.Errorf("pattern path must be absolute (got %q); pass an absolute path or a Git URL", input)
	}

	info, err := os.Stat(input)
	if err != nil {
		return nil, fmt.Errorf("pattern path %q: %w", input, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("pattern path %q is not a directory", input)
	}
	if err := validatePatternRoot(input); err != nil {
		return nil, err
	}

	return &PatternSource{Dir: input}, nil
}

// IsGitURL reports whether s looks like a Git repository URL.
func IsGitURL(s string) bool {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "git@"):
		return true
	case strings.HasPrefix(s, "git://"):
		return true
	case strings.HasPrefix(s, "ssh://"):
		return true
	case strings.HasPrefix(s, "file://"):
		return true
	case strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "https://"):
		u, err := url.Parse(s)
		return err == nil && u.Host != ""
	case strings.HasSuffix(s, ".git"):
		return strings.Contains(s, "/") && !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, ".")
	default:
		return false
	}
}

func validatePatternRoot(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "values-global.yaml")); err != nil {
		return fmt.Errorf("%s does not look like a validated pattern (missing values-global.yaml)", dir)
	}
	return nil
}

func clonePattern(gitURL string, opts *Options) (*PatternSource, error) {
	dest, reuse, err := cloneDestination(gitURL, opts)
	if err != nil {
		return nil, err
	}

	if !reuse {
		if _, err := exec.LookPath("git"); err != nil {
			return nil, fmt.Errorf("git is required to clone %s: %w", gitURL, err)
		}

		args := []string{"clone", "--depth", "1"}
		if opts.GitRef != "" {
			args = append(args, "--branch", opts.GitRef)
		}
		args = append(args, gitURL, dest)

		cmd := exec.Command("git", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("git clone %s: %w (%s)", gitURL, err, truncate(string(out), 400))
		}

		if err := validatePatternRoot(dest); err != nil {
			return nil, err
		}
	}

	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}

	return &PatternSource{Dir: abs, FromGit: true, GitURL: gitURL}, nil
}

func cloneDestination(gitURL string, opts *Options) (dest string, reuse bool, err error) {
	name := repoNameFromURL(gitURL)
	base := opts.CloneDir
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", false, err
		}
		base = cwd
	} else {
		abs, err := filepath.Abs(base)
		if err != nil {
			return "", false, err
		}
		base = abs
	}

	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", false, err
	}

	dest = filepath.Join(base, name)
	if info, err := os.Stat(dest); err == nil {
		if !info.IsDir() {
			return "", false, fmt.Errorf("clone destination %q exists and is not a directory", dest)
		}
		if entries, _ := os.ReadDir(dest); len(entries) > 0 {
			if err := validatePatternRoot(dest); err == nil {
				return dest, true, nil
			}
			return "", false, fmt.Errorf("clone destination %q already exists and is not empty", dest)
		}
		return dest, false, nil
	}
	return dest, false, nil
}

func repoNameFromURL(gitURL string) string {
	u := strings.TrimSpace(gitURL)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		return sanitizeDirName(u[i+1:])
	}
	if i := strings.LastIndex(u, ":"); i >= 0 {
		return sanitizeDirName(u[i+1:])
	}
	return "pattern"
}

func sanitizeDirName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "pattern"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
