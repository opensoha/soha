package guest

func (v *RuntimeFileResult) UnmarshalJSON(raw []byte) error {
	type wire RuntimeFileResult
	var d wire
	if err := decodeStrictBounded(raw, &d, 64<<10, "path", "contentBase64", "sha256", "size"); err != nil {
		return err
	}
	*v = RuntimeFileResult(d)
	return nil
}
func (v *BrowserFrame) UnmarshalJSON(raw []byte) error {
	type wire BrowserFrame
	var d wire
	if err := decodeStrictBounded(raw, &d, 80<<10, "controlId", "generation", "instanceId", "url", "title", "text", "jpegBase64", "width", "height"); err != nil {
		return err
	}
	*v = BrowserFrame(d)
	return nil
}
func (v *RuntimeProof) UnmarshalJSON(raw []byte) error {
	type wire RuntimeProof
	var d wire
	if err := decodeStrictBounded(raw, &d, 4096, "instanceId", "artifactSHA256", "filesystemId", "command", "fileRead", "fileWrite", "browser", "docker", "credentialsHidden", "namespacesIsolated", "dockerImage"); err != nil {
		return err
	}
	*v = RuntimeProof(d)
	return nil
}

func (v *RuntimeOperation) UnmarshalJSON(raw []byte) error {
	type wire RuntimeOperation
	var decoded wire
	if err := decodeStrictBounded(raw, &decoded, 320<<10, "schemaVersion", "operationId", "binding", "kind", "request", "deadline", "file", "browser", "dockerImage", "source", "requestHash", "artifactSHA256"); err != nil {
		return err
	}
	*v = RuntimeOperation(decoded)
	return nil
}
func (v *RuntimeFileInput) UnmarshalJSON(raw []byte) error {
	type wire RuntimeFileInput
	var decoded wire
	if err := decodeStrictBounded(raw, &decoded, 64<<10, "path", "contentBase64", "expectedSHA256"); err != nil {
		return err
	}
	*v = RuntimeFileInput(decoded)
	return nil
}
func (v *BrowserAction) UnmarshalJSON(raw []byte) error {
	type wire BrowserAction
	var decoded wire
	if err := decodeStrictBounded(raw, &decoded, 32<<10, "action", "controlId", "generation", "owner", "taskId", "url", "selector", "text", "x", "y"); err != nil {
		return err
	}
	*v = BrowserAction(decoded)
	return nil
}
func (v *RuntimeResult) UnmarshalJSON(raw []byte) error {
	type wire RuntimeResult
	var decoded wire
	if err := decodeStrictBounded(raw, &decoded, 320<<10, "schemaVersion", "operationId", "bindingId", "environment", "file", "browser", "proof"); err != nil {
		return err
	}
	*v = RuntimeResult(decoded)
	return nil
}
func (v *ToolsPlan) UnmarshalJSON(raw []byte) error {
	type wire ToolsPlan
	var decoded wire
	if err := decodeStrictBounded(raw, &decoded, 8192, "schemaVersion", "operationId", "binding", "artifactSHA256", "diskQuotaBytes", "cpuQuotaMilli", "memoryBytes", "pidsLimit", "dockerImage", "bubblewrapVersion", "dockerVersion", "chromiumVersion"); err != nil {
		return err
	}
	*v = ToolsPlan(decoded)
	return nil
}
