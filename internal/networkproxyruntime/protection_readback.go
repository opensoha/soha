package networkproxyruntime

import (
	"context"
	"encoding/json"
	"fmt"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"net/http"
	"strings"
)

func (e *engine) verifyProtection(ctx context.Context) error {
	raw, err := e.controller.request(ctx, http.MethodGet, "/configs", nil)
	if err != nil {
		return err
	}
	var config struct {
		Mode string `json:"mode"`
	}
	if json.Unmarshal(raw, &config) != nil || config.Mode != "rule" {
		return fmt.Errorf("proxy mode drifted")
	}
	if e.config.Engine != domain.EngineMihomo {
		return nil
	}
	raw, err = e.controller.request(ctx, http.MethodGet, "/rules", nil)
	if err != nil {
		return err
	}
	var result struct {
		Rules []struct {
			Type    string `json:"type"`
			Payload string `json:"payload"`
			Proxy   string `json:"proxy"`
			Extra   struct {
				Disabled bool `json:"disabled"`
			} `json:"extra"`
		} `json:"rules"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return fmt.Errorf("proxy rules readback is invalid")
	}
	expected := []string{}
	for _, host := range e.protection.Hosts {
		expected = append(expected, "DomainSuffix,"+host)
	}
	for _, cidr := range e.protection.CIDRs {
		expected = append(expected, "IPCIDR,"+cidr)
	}
	if len(result.Rules) < len(expected) {
		return fmt.Errorf("proxy protection rules missing")
	}
	for index, want := range expected {
		rule := result.Rules[index]
		if strings.Join([]string{rule.Type, rule.Payload}, ",") != want || rule.Proxy != "REJECT" || rule.Extra.Disabled {
			return fmt.Errorf("proxy protection rules drifted")
		}
	}
	return e.verifyAllMihomoRules(ctx)
}

type compiledMihomoRule struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
	Extra   struct {
		Disabled bool `json:"disabled"`
	} `json:"extra"`
}

func expectedMihomoRules(raw []byte) ([]compiledMihomoRule, error) {
	doc, err := decodeEngineDocument(domain.EngineMihomo, raw)
	if err != nil {
		return nil, err
	}
	values, err := arraySection(doc["rules"])
	if err != nil {
		return nil, err
	}
	types := map[string]string{"DOMAIN": "Domain", "DOMAIN-SUFFIX": "DomainSuffix", "DOMAIN-KEYWORD": "DomainKeyword", "IP-CIDR": "IPCIDR", "IP-CIDR6": "IPCIDR", "SRC-IP-CIDR": "SrcIPCIDR", "DST-PORT": "DstPort", "SRC-PORT": "SrcPort", "NETWORK": "Network", "PROCESS-NAME": "ProcessName", "PROCESS-PATH": "ProcessPath", "MATCH": "Match"}
	rules := make([]compiledMihomoRule, 0, len(values))
	for _, value := range values {
		line, ok := value.(string)
		if !ok {
			return nil, errUnsafeConfiguration
		}
		parts := strings.Split(line, ",")
		if len(parts) > 0 && parts[len(parts)-1] == "no-resolve" {
			parts = parts[:len(parts)-1]
		}
		if len(parts) < 2 || len(parts) > 3 || types[parts[0]] == "" {
			return nil, errUnsafeConfiguration
		}
		rule := compiledMihomoRule{Type: types[parts[0]], Proxy: parts[len(parts)-1]}
		if parts[0] != "MATCH" {
			if len(parts) != 3 {
				return nil, errUnsafeConfiguration
			}
			rule.Payload = parts[1]
		} else if len(parts) != 2 {
			return nil, errUnsafeConfiguration
		}
		rules = append(rules, rule)
	}
	return rules, nil
}
func (e *engine) verifyAllMihomoRules(ctx context.Context) error {
	raw, err := e.controller.request(ctx, http.MethodGet, "/rules", nil)
	if err != nil {
		return err
	}
	var state struct {
		Rules []compiledMihomoRule `json:"rules"`
	}
	if json.Unmarshal(raw, &state) != nil || len(state.Rules) != len(e.compiledRules) {
		return fmt.Errorf("proxy rule count drifted")
	}
	for i, got := range state.Rules {
		want := e.compiledRules[i]
		if got.Type != want.Type || got.Payload != want.Payload || got.Proxy != want.Proxy || got.Extra.Disabled {
			return fmt.Errorf("proxy complete rule order or target drifted")
		}
	}
	return nil
}
