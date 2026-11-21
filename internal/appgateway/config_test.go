package appgateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func baseTestEnv() map[string]string {
	return map[string]string{ //nolint:gosec // G101: synthetic unit test configuration map, not real credentials
		EnvAppsDomain:        "Apps.Example.Com",
		EnvCoreURL:           "http://soha-core:8080/",
		EnvPublicCoreURL:     "https://soha.example.com/",
		EnvToken:             "gw-token-initial",
		EnvCookieSecret:      "0123456789abcdef0123456789abcdef",
		EnvS3Endpoint:        "http://minio.internal:9000/",
		EnvS3Bucket:          "soha-hosted-apps",
		EnvS3AccessKeyID:     "minio-ro-ak",
		EnvS3SecretAccessKey: "minio-ro-sk",
	}
}

func TestLoadConfigDefaultsAndNormalization(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfigFromLookup(mapLookup(baseTestEnv()))
	require.NoError(t, err)
	require.Equal(t, ":8090", cfg.ListenAddr)
	require.Equal(t, ":8091", cfg.HealthAddr)
	require.Empty(t, cfg.HTTPRedirectAddr)
	require.Equal(t, "apps.example.com", cfg.AppsDomain)
	require.Equal(t, "https", cfg.PublicScheme)
	require.Equal(t, "http://soha-core:8080", cfg.CoreURL)
	require.Equal(t, "https://soha.example.com", cfg.PublicCoreURL)
	require.Equal(t, "http://minio.internal:9000", cfg.S3Endpoint)
	require.Equal(t, "us-east-1", cfg.S3Region)
	require.True(t, cfg.S3PathStyle)
	require.False(t, cfg.S3Insecure)
	require.Equal(t, time.Duration(0), cfg.DecisionCacheTTL)

	env := baseTestEnv()
	env[EnvAppsDomain] = "*.Apps.Example.Com."
	env[EnvDecisionCacheTTL] = "2s"
	env[EnvPublicScheme] = "HTTP"
	env[EnvTrustedProxies] = "10.42.0.0/16, 192.168.1.10, fd00::/64"
	env[EnvS3Prefix] = "/hosted-apps/"
	custom, err := LoadConfigFromLookup(mapLookup(env))
	require.NoError(t, err)
	require.Equal(t, "apps.example.com", custom.AppsDomain)
	require.Equal(t, "http", custom.PublicScheme)
	require.Equal(t, 2*time.Second, custom.DecisionCacheTTL)
	require.Equal(t, "hosted-apps", custom.S3Prefix)
	require.Len(t, custom.trustedProxyPrefixes, 3)
}

func TestLoadConfigRejectsInvalidSettings(t *testing.T) {
	t.Parallel()

	invalidCases := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"missing apps domain", func(m map[string]string) { delete(m, EnvAppsDomain) }},
		{"single label apps domain", func(m map[string]string) { m[EnvAppsDomain] = "localhost" }},
		{"IP literal apps domain", func(m map[string]string) { m[EnvAppsDomain] = "127.0.0.1" }},
		{"URL scheme in apps domain", func(m map[string]string) { m[EnvAppsDomain] = "https://apps.example.com" }},
		{"invalid public scheme", func(m map[string]string) { m[EnvPublicScheme] = "ftp" }},
		{"short cookie secret", func(m map[string]string) { m[EnvCookieSecret] = "too-short-secret" }},
		{"missing gateway token", func(m map[string]string) { delete(m, EnvToken) }},
		{"invalid trusted proxy CIDR", func(m map[string]string) { m[EnvTrustedProxies] = "10.0.0.0/99" }},
		{"unpaired TLS cert file", func(m map[string]string) { m[EnvTLSCertFile] = "/tmp/tls.crt" }},
		{"colliding redirect and listen addr", func(m map[string]string) { m[EnvHTTPRedirectAddr] = ":8090" }},
		{"missing S3 bucket", func(m map[string]string) { delete(m, EnvS3Bucket) }},
		{"traversal in S3 prefix", func(m map[string]string) { m[EnvS3Prefix] = "hosted/../escape" }},
		{"negative cache TTL", func(m map[string]string) { m[EnvDecisionCacheTTL] = "-1s" }},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := baseTestEnv()
			tc.mutate(env)
			_, err := LoadConfigFromLookup(mapLookup(env))
			require.Error(t, err)
		})
	}
}

func TestSecretProviderHotReloadAndTLSCertificate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "gateway-token")
	cookieFile := filepath.Join(dir, "cookie-secret")
	akFile := filepath.Join(dir, "access-key-id")
	skFile := filepath.Join(dir, "secret-access-key")
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")

	writeSecretFiles(t, tokenFile, cookieFile, akFile, skFile, "  token-v1 \n", "cookie-secret-v1-0123456789abcdef\n", "ak-v1\n", "sk-v1\n")
	certPEM1, keyPEM1 := generateSelfSignedCertPEM(t, "*.apps.example.com", 1)
	require.NoError(t, os.WriteFile(certFile, certPEM1, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM1, 0o600))

	provider := newSecretProvider(Config{
		TokenFile:        tokenFile,
		CookieSecretFile: cookieFile,
		S3AccessKeyFile:  akFile,
		S3SecretKeyFile:  skFile,
		TLSCertFile:      certFile,
		TLSKeyFile:       keyFile,
	})
	require.NoError(t, provider.ValidateAll(true))
	tok, err := provider.GatewayToken()
	require.NoError(t, err)
	require.Equal(t, "token-v1", tok)
	loadedCert1, err := provider.LoadTLSCertificate()
	require.NoError(t, err)

	writeSecretFiles(t, tokenFile, cookieFile, akFile, skFile, "token-v2\n", "cookie-secret-v2-fedcba9876543210\n", "ak-v2\n", "sk-v2\n")
	certPEM2, keyPEM2 := generateSelfSignedCertPEM(t, "*.apps.example.com", 2)
	require.NoError(t, os.WriteFile(certFile, certPEM2, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM2, 0o600))

	tok2, err := provider.GatewayToken()
	require.NoError(t, err)
	require.Equal(t, "token-v2", tok2)
	cookie2, err := provider.CookieSecret()
	require.NoError(t, err)
	require.Equal(t, "cookie-secret-v2-fedcba9876543210", string(cookie2))
	ak2, sk2, err := provider.S3Credentials()
	require.NoError(t, err)
	require.Equal(t, "ak-v2", ak2)
	require.Equal(t, "sk-v2", sk2)
	loadedCert2, err := provider.LoadTLSCertificate()
	require.NoError(t, err)
	require.NotSame(t, loadedCert1, loadedCert2)
}

func writeSecretFiles(t *testing.T, tokenFile, cookieFile, akFile, skFile, tok, cookie, ak, sk string) {
	t.Helper()
	require.NoError(t, os.WriteFile(tokenFile, []byte(tok), 0o600))
	require.NoError(t, os.WriteFile(cookieFile, []byte(cookie), 0o600))
	require.NoError(t, os.WriteFile(akFile, []byte(ak), 0o600))
	require.NoError(t, os.WriteFile(skFile, []byte(sk), 0o600))
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

func generateSelfSignedCertPEM(t *testing.T, dnsName string, serial int64) ([]byte, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}
