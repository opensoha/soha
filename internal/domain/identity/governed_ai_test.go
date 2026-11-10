package identity

import "testing"

func TestParseGovernedAIBinding(t *testing.T) {
	for _, value := range []any{nil, "gateway_only", map[string]any{}, map[string]any{"mode": "native_rest", "aiClientId": "codex", "skillId": "k8s-sre"}, map[string]any{"mode": "gateway_only", "aiClientId": "codex", "skillId": ""}, map[string]any{"mode": "gateway_only", "aiClientId": "codex", "skillId": "k8s-sre", "extra": true}} {
		if _, err := ParseGovernedAIBinding(map[string]any{"governedAI": value}); err == nil {
			t.Fatalf("invalid binding accepted: %#v", value)
		}
	}
	if binding, err := ParseGovernedAIBinding(map[string]any{"label": "human"}); binding != nil || err != nil {
		t.Fatal("ordinary credential metadata changed")
	}
	binding, err := ParseGovernedAIBinding(map[string]any{"governedAI": map[string]any{"mode": "gateway_only", "aiClientId": "codex", "skillId": "k8s-sre"}})
	if err != nil || binding == nil || binding.AIClientID != "codex" || binding.SkillID != "k8s-sre" {
		t.Fatal("valid binding rejected")
	}
}
