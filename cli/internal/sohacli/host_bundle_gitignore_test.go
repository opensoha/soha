package sohacli

import (
	"slices"
	"strings"
	"testing"
)

func TestHostDynamicBundleAppliesProjectIgnoreRulesBeforePreflight(t *testing.T) {
	root := writeHostBundleContentFixture(t, map[string]string{
		"soha-host.json":            `{"template":"python","database":false}`,
		"app.py":                    "print('ready')",
		"requirements.txt":          "",
		".gitignore":                ".env\n*.local.txt\n!keep.local.txt\nscratch/\n!scratch/nested.py\n!node_modules/\n",
		".env":                      "api_key = 'synthetic-ignored-credential'",
		"discard.local.txt":         "not for release",
		"keep.local.txt":            "kept",
		"scratch/nested.py":         "api_key = 'synthetic-nested-credential'",
		"node_modules/lib/app.js":   "api_key = 'synthetic-dependency-credential'",
		"service/.gitignore":        "!include.local.txt\n",
		"service/include.local.txt": "kept by child",
		"service/discard.local.txt": "excluded by ancestor rule",
	})
	bundle, err := buildHostDynamicBundle(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app.py", "keep.local.txt", "requirements.txt", "service/include.local.txt", "soha-host.json"}
	if got := readTarGzMemberNames(t, bundle.Archive); !slices.Equal(got, want) {
		t.Fatalf("bundle files = %v, want %v", got, want)
	}

	invalid := writeHostBundleContentFixture(t, map[string]string{
		"soha-host.json":   `{"template":"python","database":false}`,
		"app.py":           "print('ready')",
		"requirements.txt": "",
		".gitignore":       "!.env\n",
		".env":             "synthetic fixture",
	})
	if _, err := buildHostDynamicBundle(invalid); err == nil || !strings.Contains(err.Error(), "sensitive file") {
		t.Fatalf("included secret file must still be blocked: %v", err)
	}
}
