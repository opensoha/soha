package sohacli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostBundleContentDryRunRejectsCredentialTextWithoutEcho(t *testing.T) {
	syntheticAWSKey := "AKIA" + "IOSFODNN7EXAMPLE"
	testCases := []struct {
		name     string
		kind     string
		files    map[string]string
		secret   string
		detector string
	}{
		{
			name: "static aws access key",
			kind: "static",
			files: map[string]string{
				"index.html":    "<!doctype html><h1>ok</h1>",
				"assets/app.js": "const key = '" + syntheticAWSKey + "';",
			},
			secret:   syntheticAWSKey,
			detector: "aws_access_key",
		},
		{
			name: "dynamic credential assignment",
			kind: "dynamic",
			files: map[string]string{
				"soha-host.json":   `{"template":"python","database":false}`,
				"app.py":           "print('ok')\n",
				"requirements.txt": "flask==3.0.0\n",
				"config.py":        "api_key = 'not-for-upload'\n",
			},
			secret:   "not-for-upload",
			detector: "credential_assignment",
		},
		{
			name: "static private key marker",
			kind: "static",
			files: map[string]string{
				"index.html": "<pre>-----BEGIN " + "RSA PRIVATE KEY-----</pre>",
			},
			secret:   "BEGIN " + "RSA PRIVATE KEY",
			detector: "private_key",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeHostBundleContentFixture(t, tc.files)
			var out, errBuf bytes.Buffer
			code := Run(context.Background(), []string{"host", "deploy", dir, "--kind", tc.kind, "--dry-run"}, Runtime{
				In:  strings.NewReader(""),
				Out: &out,
				Err: &errBuf,
			})
			if code != 1 {
				t.Fatalf("Run code = %d, want 1; out=%s err=%s", code, out.String(), errBuf.String())
			}
			diagnostic := errBuf.String()
			if !strings.Contains(diagnostic, tc.detector) || !strings.Contains(diagnostic, "redacted") {
				t.Fatalf("expected detector and redaction notice, got %q", diagnostic)
			}
			if strings.Contains(diagnostic, tc.secret) || strings.Contains(out.String(), tc.secret) {
				t.Fatal("matching credential content was echoed")
			}
			if strings.Contains(out.String(), "Local scan: passed") {
				t.Fatal("dry-run reported success for a bundle with credential content")
			}
		})
	}
}

func TestHostBundleContentAllowsNormalStaticAndDynamicBundles(t *testing.T) {
	staticDir := writeHostBundleContentFixture(t, map[string]string{
		"index.html":       "<!doctype html><script type=\"module\" src=\"assets/app.js\"></script>",
		"assets/app.js":    "document.body.dataset.ready = 'true';",
		"assets/app.css":   "body { font-family: sans-serif; }",
		"assets/logo.svg":  "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>",
		"assets/pixel.png": "PNG synthetic bytes with password = 'not-real-text'\x00\x01",
	})
	if _, err := buildHostStaticBundle(staticDir); err != nil {
		t.Fatalf("normal static bundle rejected: %v", err)
	}

	dynamicDir := writeHostBundleContentFixture(t, map[string]string{
		"soha-host.json":   `{"template":"python","database":false}`,
		"app.py":           "from flask import Flask\napp = Flask(__name__)\n",
		"requirements.txt": "flask==3.0.0\n",
	})
	if _, err := buildHostDynamicBundle(dynamicDir); err != nil {
		t.Fatalf("normal dynamic bundle rejected: %v", err)
	}
}

func writeHostBundleContentFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for relPath, content := range files {
		fullPath := filepath.Join(dir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
