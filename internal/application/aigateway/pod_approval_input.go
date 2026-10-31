package aigateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
)

// Keep exact argv/stdin only in the existing encrypted approval envelope.
// The complete input digest binds even omitted or unknown display properties.
func podExecutionApprovalDisplay(input map[string]any) map[string]any {
	out := map[string]any{"inputDigest": protectedPodInputDigest(input)}
	for _, key := range []string{"clusterId", "namespace", "podName", "idempotencyKey"} {
		out[key] = input[key]
	}
	execution := mapValue(input["execution"])
	display := map[string]any{}
	for _, key := range []string{"expectedUid", "container", "timeoutSeconds", "maxOutputBytes"} {
		if value, exists := execution[key]; exists {
			display[key] = value
		}
	}
	display["argv"] = protectedPodInputDigest(execution["argv"])
	argv := reflect.ValueOf(execution["argv"])
	if argv.IsValid() && (argv.Kind() == reflect.Slice || argv.Kind() == reflect.Array) {
		display["argvCount"] = argv.Len()
	}
	stdin, _ := execution["stdinBase64"].(string)
	data, _ := base64.StdEncoding.Strict().DecodeString(stdin)
	display["stdinBase64"] = protectedPodInputDigest(stdin)
	display["stdinBytes"] = len(data)
	out["execution"] = display
	return sanitizeGatewayMap(out)
}

func protectedPodInputDigest(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "[PROTECTED INVALID INPUT]"
	}
	return fmt.Sprintf("[PROTECTED sha256:%x]", sha256.Sum256(raw))
}
