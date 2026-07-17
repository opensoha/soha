package access

import "strings"

// MatchesNamespaceSelector evaluates the equality/existence subset used by
// platform scope grants. Invalid or empty requirements fail closed.
func MatchesNamespaceSelector(selector string, labels map[string]string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return true
	}
	if !ValidNamespaceSelector(selector) {
		return false
	}
	for _, rawRequirement := range strings.Split(selector, ",") {
		requirement := strings.TrimSpace(rawRequirement)
		if !matchesNamespaceRequirement(requirement, labels) {
			return false
		}
	}
	return true
}

// ValidNamespaceSelector validates the intentionally small equality and
// existence selector subset accepted by platform scope grants.
func ValidNamespaceSelector(selector string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return true
	}
	for _, rawRequirement := range strings.Split(selector, ",") {
		if !validNamespaceRequirement(strings.TrimSpace(rawRequirement)) {
			return false
		}
	}
	return true
}

func validNamespaceRequirement(requirement string) bool {
	if requirement == "" {
		return false
	}
	if strings.HasPrefix(requirement, "!") && !strings.Contains(requirement, "!=") {
		return validSelectorToken(strings.TrimPrefix(requirement, "!"))
	}
	for _, operator := range []string{"!=", "==", "="} {
		if key, value, found := strings.Cut(requirement, operator); found {
			return validSelectorToken(key) && strings.TrimSpace(value) != ""
		}
	}
	return validSelectorToken(requirement)
}

func validSelectorToken(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, " \t\r\n")
}

func matchesNamespaceRequirement(requirement string, labels map[string]string) bool {
	if strings.HasPrefix(requirement, "!") && !strings.Contains(requirement, "!=") {
		_, exists := labels[strings.TrimSpace(strings.TrimPrefix(requirement, "!"))]
		return !exists
	}
	if key, value, found := strings.Cut(requirement, "!="); found {
		actual, exists := labels[strings.TrimSpace(key)]
		return !exists || actual != strings.TrimSpace(value)
	}
	if key, value, found := strings.Cut(requirement, "=="); found {
		actual, exists := labels[strings.TrimSpace(key)]
		return exists && actual == strings.TrimSpace(value)
	}
	if key, value, found := strings.Cut(requirement, "="); found {
		actual, exists := labels[strings.TrimSpace(key)]
		return exists && actual == strings.TrimSpace(value)
	}
	_, exists := labels[requirement]
	return exists
}
