package companionartifact

import "testing"

func TestValidatePackageURLRejectsUnsafeTargets(t *testing.T) {
	for _, value := range []string{
		"http://example.com/model.zip",
		"https://user:password@example.com/model.zip",
		"https://127.0.0.1/model.zip",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/model.zip",
		"https://192.0.2.1/model.zip",
		"https://198.51.100.1/model.zip",
		"https://203.0.113.1/model.zip",
		"https://[::1]/model.zip",
		"https://[2001:db8::1]/model.zip",
	} {
		if err := validatePackageURL(value); err == nil {
			t.Errorf("validatePackageURL(%q) error = nil, want rejection", value)
		}
	}
}

func TestValidatePackageURLAllowsPublicHTTPS(t *testing.T) {
	if err := validatePackageURL("https://cdn.example.com/companion/model.zip"); err != nil {
		t.Fatalf("validatePackageURL() error = %v", err)
	}
}
