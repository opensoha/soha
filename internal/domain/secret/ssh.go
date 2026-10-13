package secret

type Kind string

const (
	KindOpaque       Kind = "opaque"
	KindSSHKeypair   Kind = "ssh_keypair"
	KindSSHPublicKey Kind = "ssh_public_key"
	KindSSHPassword  Kind = "ssh_password"
)

type SSHPublicKey struct {
	AuthorizedKey string `json:"authorizedKey"`
	Algorithm     string `json:"algorithm"`
	Fingerprint   string `json:"fingerprint"`
}

type SSHKeypairMaterial struct {
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase,omitempty"`
}
