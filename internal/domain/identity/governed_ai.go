package identity

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// GovernedAIBinding is loaded from the stored credential, never request headers.
type GovernedAIBinding struct {
	Mode       string `json:"mode"`
	AIClientID string `json:"aiClientId"`
	SkillID    string `json:"skillId"`
}

var governedAIIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func ParseGovernedAIBinding(metadata map[string]any) (*GovernedAIBinding, error) {
	value, found := metadata["governedAI"]
	if !found {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("invalid governed AI credential binding")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 {
		return nil, fmt.Errorf("governed AI binding requires only mode, aiClientId and skillId")
	}
	var binding GovernedAIBinding
	if json.Unmarshal(raw, &binding) != nil || binding.Mode != "gateway_only" || !governedAIIdentifier.MatchString(binding.AIClientID) || !governedAIIdentifier.MatchString(binding.SkillID) {
		return nil, fmt.Errorf("invalid governed AI credential binding")
	}
	return &binding, nil
}
