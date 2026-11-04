package sohaapi

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestToolInvocationResultPreservesAbsentVersion(t *testing.T) {
	for _, input := range []string{
		`{"toolName":"legacy","riskLevel":"read","requiresApproval":false,"result":"success"}`,
		`{"toolName":"versioned","capabilityVersion":"1","riskLevel":"read","requiresApproval":false,"result":"success"}`,
	} {
		var value ToolInvocationResult
		if err := json.Unmarshal([]byte(input), &value); err != nil {
			t.Fatal(err)
		}
		output, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]any
		if err := json.Unmarshal([]byte(input), &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(output, &after); err != nil {
			t.Fatal(err)
		}
		if before["capabilityVersion"] != after["capabilityVersion"] {
			t.Fatalf("version presence changed during round trip: %s", output)
		}
	}
}

func TestDeliveryWorkflowPreservesExplicitFalse(t *testing.T) {
	for _, value := range []string{"false", "true"} {
		var definition DeliveryWorkflowDefinition
		if err := json.Unmarshal([]byte(`{"name":"delivery","targets":[],"stopOnFailure":`+value+`}`), &definition); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || string(fields["stopOnFailure"]) != value {
			t.Fatalf("stopOnFailure=%s was lost: %s (%v)", value, raw, err)
		}
	}
}

func TestKubernetesWorkloadSnapshotRequestGeneratedShape(t *testing.T) {
	request := KubernetesWorkloadSnapshotRequest{
		Namespace:     "default",
		SourceKind:    KubernetesWorkloadSnapshotSourceKindDeployment,
		SourceName:    "reports",
		TargetKind:    KubernetesWorkloadSnapshotTargetKindCronJob,
		TargetName:    "reports-schedule",
		Inherit:       []KubernetesWorkloadSnapshotInheritance{KubernetesWorkloadSnapshotInheritanceEnvironment},
		RestartPolicy: KubernetesWorkloadSnapshotRestartPolicyNever,
	}
	if request.Namespace != "default" || len(request.Inherit) != 1 {
		t.Fatalf("request = %#v", request)
	}
}

func TestWorkbenchMessageDoneEventRoleGeneratedName(t *testing.T) {
	if WorkbenchMessageDoneEventRoleAssistant != WorkbenchMessageDoneEventRole("assistant") {
		t.Fatalf("role = %q", WorkbenchMessageDoneEventRoleAssistant)
	}
}

func TestWorkbenchMessageDeltaEventLegacyRoleName(t *testing.T) {
	if Assistant != WorkbenchMessageDeltaEventRole("assistant") {
		t.Fatalf("role = %q", Assistant)
	}
}

func TestKubernetesResourceSearchGeneratedShape(t *testing.T) {
	result := KubernetesResourceSearchResult{
		Items: []KubernetesResourceSearchItem{
			{
				Resource: KubernetesResourceRef{
					APIVersion: "v1",
					ClusterID:  "cluster-a",
					Kind:       "Pod",
					Name:       "api-0",
					Namespace:  "production",
					ScopeMode:  KubernetesResourceScopeModeNamespace,
				},
				Status: "Running",
			},
		},
		Truncated: false,
	}
	if len(result.Items) != 1 || result.Items[0].Resource.Name != "api-0" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDeliveryBatchInputPreservesSourceChoice(t *testing.T) {
	for _, raw := range []string{
		`{"idempotencyKey":"attempt-1","definition":{"name":"release","targets":[]}}`,
		`{"idempotencyKey":"attempt-2","workflowId":"workflow-1","workflowVersion":2}`,
	} {
		var input DeliveryBatchInput
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		_, definition := fields["definition"]
		_, workflow := fields["workflowId"]
		_, version := fields["workflowVersion"]
		if definition == workflow || workflow != version {
			t.Fatalf("source choice changed: %s", encoded)
		}
	}
}

func TestLegacyEnumAliasesRemainSourceCompatible(t *testing.T) {
	if Allow != ScopeGrantEffect("allow") || Deny != ScopeGrantEffect("deny") {
		t.Fatalf("scope grant effect aliases = %q/%q", Allow, Deny)
	}
	if ListAIGatewayRelayModelCallsParamsStatusSuccess != ListAIGatewayRelayModelCallsParamsStatus("success") ||
		ListAIGatewayRelayModelCallsParamsStatusFailure != ListAIGatewayRelayModelCallsParamsStatus("failure") ||
		ListAIGatewayRelayModelCallsParamsStatusCancelled != ListAIGatewayRelayModelCallsParamsStatus("cancelled") ||
		ListAIGatewayRelayModelCallsParamsStatusClientCancelled != ListAIGatewayRelayModelCallsParamsStatus("client_cancelled") ||
		ListAIGatewayRelayModelCallsParamsStatusRateLimited != ListAIGatewayRelayModelCallsParamsStatus("rate_limited") ||
		ListAIGatewayRelayModelCallsParamsStatusPolicyDenied != ListAIGatewayRelayModelCallsParamsStatus("policy_denied") {
		t.Fatalf("relay model call status aliases changed")
	}
	if TCP != DockerContainerPortInputProtocolTCP || UDP != DockerContainerPortInputProtocolUDP {
		t.Fatalf("docker protocol aliases = %q/%q", TCP, UDP)
	}
	if Critical != MarketplaceAdvisorySeverityCritical ||
		High != MarketplaceAdvisorySeverityHigh ||
		Medium != MarketplaceAdvisorySeverityMedium ||
		Low != MarketplaceAdvisorySeverityLow {
		t.Fatalf("marketplace severity aliases = %q/%q/%q/%q", Critical, High, Medium, Low)
	}
	if ListAIGatewayRelayUpstreamsParamsStatusActive != Active ||
		ListAIGatewayRelayUpstreamsParamsStatusDegraded != Degraded ||
		ListAIGatewayRelayUpstreamsParamsStatusDisabled != Disabled {
		t.Fatalf("relay status aliases changed")
	}
}

func TestApprovalScopeKeepsMapCompatibility(t *testing.T) {
	var item ApprovalRequest
	if err := json.Unmarshal([]byte(`{"resourceScope":{"projectId":"project-1","invocationScopes":[{"hostId":"host-1","projectId":"project-1"}]}}`), &item); err != nil {
		t.Fatal(err)
	}
	scope := item.ResourceScope
	if len(scope) != 2 || scope["projectId"] != "project-1" {
		t.Fatalf("scope lost: %v", scope)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var again ApprovalRequest
	if err := json.Unmarshal(encoded, &again); err != nil {
		t.Fatal(err)
	}
	scopes, ok := again.ResourceScope["invocationScopes"].([]any)
	if !ok || len(scopes) != 1 {
		t.Fatalf("resolved scopes lost: %s", encoded)
	}
	scope, ok = scopes[0].(map[string]any)
	if !ok || scope["hostId"] != "host-1" {
		t.Fatalf("resolved scope lost: %s", encoded)
	}
}

func TestNetworkProxyInputsRemainInGoSDK(t *testing.T) {
	for _, input := range []struct {
		raw   string
		value any
	}{
		{`{"id":"proxy-a","name":"Proxy A","engine":"mihomo","host":"host-a"}`, &NetworkProxyInstanceInput{}},
		{`{"expectedRevision":3,"enabled":false,"content":"mode: rule"}`, &NetworkProxyConfigurationInput{}},
	} {
		if err := json.Unmarshal([]byte(input.raw), input.value); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(input.value)
		if err != nil {
			t.Fatal(err)
		}
		var before, after map[string]any
		if err := json.Unmarshal([]byte(input.raw), &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &after); err != nil {
			t.Fatal(err)
		}
		for name, value := range before {
			if after[name] != value {
				t.Fatalf("%s changed during round trip: %s", name, encoded)
			}
		}
	}
}

func TestEnvironmentServiceReferenceInputRoundTrip(t *testing.T) {
	for _, fixture := range []struct {
		name string
		read func() ([]byte, error)
	}{
		{"activate", func() ([]byte, error) {
			return os.ReadFile("../../../fixtures/openapi/EnvironmentServiceReferenceInput/valid/activate.json")
		}},
		{"explicit-rebind", func() ([]byte, error) {
			return os.ReadFile("../../../fixtures/openapi/EnvironmentServiceReferenceInput/valid/explicit-rebind.json")
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			raw, err := fixture.read()
			if err != nil {
				t.Fatal(err)
			}
			var input EnvironmentServiceReferenceInput
			if err := json.Unmarshal(raw, &input); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var before, after map[string]any
			if err := json.Unmarshal(raw, &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &after); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("reference CAS fields or presence changed: %s", encoded)
			}
		})
	}
}

func TestDeliveryReferenceEnumCompatibility(t *testing.T) {
	for _, value := range []DeliveryPlanStatus{Confirmed, Confirming, Draft, WaitingApproval} {
		if !value.Valid() {
			t.Fatalf("published Plan status %q is invalid", value)
		}
	}
	for _, value := range []PersonalAgentAccessDecisionStatus{Approved, Rejected, Revoked} {
		if !value.Valid() {
			t.Fatalf("published access decision %q is invalid", value)
		}
	}
}
