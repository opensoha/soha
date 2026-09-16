package application

// GitSourceAccess is an execution-only grant from the registered connection.
// Repository definitions and documents cannot supply its endpoint or secrets.
type GitSourceAccess struct {
	RepositoryURL        string            `json:"-"`
	Scheme               string            `json:"-"`
	Host                 string            `json:"-"`
	Port                 string            `json:"-"`
	Address              string            `json:"-"`
	CertificateAuthority string            `json:"-"`
	Credentials          map[string]string `json:"-"`
}
