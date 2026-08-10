package airgap

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	ociSkipRe = regexp.MustCompile(`(?i)(?:SKIPPING|skipped).*?(oci://[^\s'"]+)`)
	ociURLRe  = regexp.MustCompile(`oci://[^\s'"]+`)
)

// ParseMirrorLog returns OCI-related images skipped in an oc-mirror log.
func ParseMirrorLog(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	found := map[string]struct{}{}
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(strings.ToLower(line), "oci") {
			continue
		}
		if !strings.Contains(strings.ToLower(line), "skip") && !strings.Contains(line, "not supported") {
			continue
		}
		for _, m := range ociURLRe.FindAllString(line, -1) {
			img := strings.TrimPrefix(m, "oci://")
			img = strings.TrimRight(img, ".,;:'\")")
			if img != "" {
				found[img] = struct{}{}
			}
		}
		if sm := ociSkipRe.FindStringSubmatch(line); len(sm) > 1 {
			img := strings.TrimPrefix(sm[1], "oci://")
			img = strings.TrimRight(img, ".,;:'\")")
			if img != "" {
				found[img] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(found))
	for img := range found {
		out = append(out, img)
	}
	sort.Strings(out)
	return out, nil
}

// FormatSkippedImagesYAML returns an additionalImages YAML snippet for skipped OCI images.
func FormatSkippedImagesYAML(images []string) string {
	if len(images) == 0 {
		return "# No skipped oci:// related images found in the log.\n"
	}
	var b strings.Builder
	b.WriteString("# Add these under mirror.additionalImages in imagesetconfig.yaml\n")
	b.WriteString("additionalImages:\n")
	for _, img := range images {
		fmt.Fprintf(&b, "  - name: %s\n", img)
	}
	return b.String()
}
