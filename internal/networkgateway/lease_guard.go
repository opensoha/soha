package networkgateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
)

// The kernel expires the singleton even if the userspace gateway crashes. The
// lookup masks the packet mark without changing it; every allow and return path
// needs the same still-live element, while deny rules remain permanent.
func nftLeaseGuardProgram(wg networkprotocol.WireGuardConfiguration, deadlines []time.Time) (string, error) {
	base := "add set inet " + firewallTable + " lease_guard { type mark; flags timeout;"
	if len(wg.FirewallRules) == 0 && len(deadlines) == 0 {
		return base + " }", nil
	}
	if len(deadlines) != 1 {
		return "", errors.New("kernel lease deadline is required")
	}
	seconds := int(time.Until(deadlines[0]) / time.Second)
	if seconds < 1 || seconds > 300 {
		return "", errors.New("kernel lease deadline is outside safe bounds")
	}
	return fmt.Sprintf("%s elements = { 0 timeout %ds }; }", base, seconds), nil
}

func nftLeaseGuardMatch() map[string]any {
	return nftMatch(map[string]any{"&": []any{nftMeta("mark"), float64(0)}}, "@lease_guard")
}

func verifyNftLeaseGuard(raw []byte, deadlines []time.Time) error {
	var document struct {
		Nftables []struct {
			Set *struct {
				Family, Table, Name, Type string
				Flags                     []string
				Elem                      []json.RawMessage `json:"elem"`
			} `json:"set"`
		} `json:"nftables"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return errors.New("invalid lease guard readback")
	}
	count := 0
	for _, item := range document.Nftables {
		if item.Set == nil {
			continue
		}
		set := item.Set
		count++
		if set.Family != "inet" || set.Table != firewallTable || set.Name != "lease_guard" || set.Type != "mark" || len(set.Flags) != 1 || !slices.Contains(set.Flags, "timeout") || len(set.Elem) != 1 {
			return errors.New("kernel lease guard differs from desired state")
		}
		var element struct {
			Elem struct {
				Val     any     `json:"val"`
				Timeout float64 `json:"timeout"`
				Expires float64 `json:"expires"`
			} `json:"elem"`
		}
		if json.Unmarshal(set.Elem[0], &element) != nil || !zeroNftMark(element.Elem.Val) || element.Elem.Timeout <= 0 || element.Elem.Expires <= 0 || element.Elem.Expires > element.Elem.Timeout || element.Elem.Timeout > 300 {
			return errors.New("kernel lease guard expiry is invalid")
		}
		if len(deadlines) == 1 && element.Elem.Expires > time.Until(deadlines[0]).Seconds()+1 {
			return errors.New("kernel lease guard outlives authorization")
		}
	}
	if count != 1 {
		return errors.New("kernel lease guard is missing or ambiguous")
	}
	return nil
}

func zeroNftMark(value any) bool {
	switch v := value.(type) {
	case float64:
		return v == 0
	case string:
		return v == "0" || v == "0x00000000" || v == "0x0"
	}
	return false
}
