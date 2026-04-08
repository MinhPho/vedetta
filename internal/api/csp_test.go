package api

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestContentSecurityPolicy_DisablesInlineScriptAttributes(t *testing.T) {
	csp := contentSecurityPolicy()
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP unexpectedly allows inline scripts: %s", csp)
	}
	if !strings.Contains(csp, "script-src-attr 'none'") {
		t.Fatalf("CSP missing script-src-attr restriction: %s", csp)
	}
	if !strings.Contains(csp, "https://unpkg.com") {
		t.Fatalf("CSP missing allowed external HTMX origin: %s", csp)
	}
	if !strings.Contains(csp, "'sha256-") {
		t.Fatalf("CSP missing inline script hashes: %s", csp)
	}
}

func TestStaticAssets_DoNotUseInlineEventHandlers(t *testing.T) {
	pattern := regexp.MustCompile(`\s(onclick|onchange|oninput|onfocus|onkeydown|onload|onerror|onsubmit)\s*=|hx-on::`)

	checkFile := func(path string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if pattern.Match(data) {
			t.Fatalf("found inline handler in %s", path)
		}
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	apiDir := filepath.Dir(thisFile)
	staticDir := filepath.Join(apiDir, "static")
	err := filepath.WalkDir(staticDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".html") || strings.HasSuffix(path, ".js") {
			checkFile(path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk static dir: %v", err)
	}

	checkFile(filepath.Join(apiDir, "handler_partials.go"))
}
