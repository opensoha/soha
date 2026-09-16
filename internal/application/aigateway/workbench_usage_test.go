package aigateway

import "testing"

func TestWorkbenchReportedUsagePreservesMissingMeasurement(t *testing.T) {
	if workbenchReportedUsage([]byte(`{"choices":[]}`)) != nil {
		t.Fatal("missing usage must remain unknown")
	}
	usage := workbenchReportedUsage([]byte(`{"usage":{"prompt_tokens":13,"completion_tokens":4,"total_tokens":17}}`))
	if usage["inputTokens"] != 13 || usage["outputTokens"] != 4 || usage["totalTokens"] != 17 {
		t.Fatalf("reported usage: %v", usage)
	}
}
