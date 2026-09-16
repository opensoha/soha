package docker

// PreparedDeliveryProject is persisted only inside the server's frozen batch.
// Ciphertext contains Compose and environment values; public plans expose only digests.
type PreparedDeliveryProject struct {
	HostID string `json:"hostId"`
	ProjectID string `json:"projectId"`
	ProjectDigest string `json:"projectDigest"`
	RenderedDigest string `json:"renderedDigest,omitempty"`
	ExpectedServices []string `json:"expectedServices,omitempty"`
	Images map[string]string `json:"images,omitempty"`
	Ciphertext string `json:"ciphertext"`
}
