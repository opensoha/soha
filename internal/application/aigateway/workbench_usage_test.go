package aigateway

import "testing"

func TestWorkbenchAccountedUsageEstimatesMissingSyncAndStreamMeasurement(t *testing.T) {
	request := LLMRelayHTTPRequest{Endpoint: "chat/completions", Body: []byte(`{"messages":[{"role":"user","content":"查找运行证据"}]}`)}
	for _, output := range []string{
		`{"choices":[{"message":{"content":"evidence receipt"}}]}`,
		"data: {\"choices\":[{\"delta\":{\"content\":\"evidence receipt\"}}]}\n\ndata: [DONE]\n\n",
	} {
		usage, estimated := workbenchAccountedUsage(request, []byte(output), nil)
		if !estimated || usage["totalTokens"] <= 0 || usage["inputTokens"] <= 0 {
			t.Fatalf("missing usage became free or exact: %v estimated=%v", usage, estimated)
		}
	}
	usage, estimated := workbenchAccountedUsage(request, nil, map[string]float64{"totalTokens": 17})
	if estimated || usage["totalTokens"] != 17 {
		t.Fatal("reported measurement was replaced by an estimate")
	}
}

func TestWorkbenchReportedUsagePreservesMissingMeasurement(t *testing.T) {
	if workbenchReportedUsage([]byte(`{"choices":[]}`)) != nil {
		t.Fatal("missing usage must remain unknown")
	}
	usage := workbenchReportedUsage([]byte(`{"usage":{"prompt_tokens":13,"completion_tokens":4,"total_tokens":17}}`))
	if usage["inputTokens"] != 13 || usage["outputTokens"] != 4 || usage["totalTokens"] != 17 {
		t.Fatalf("reported usage: %v", usage)
	}
}
