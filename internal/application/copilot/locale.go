package copilot

import (
	"strings"
	"unicode"
)

func normalizeLocale(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "zh") {
		return "zh-CN"
	}
	return "en-US"
}

func localize(locale, zh, en string) string {
	if normalizeLocale(locale) == "zh-CN" {
		return zh
	}
	return en
}

func detectMessageLocale(prompt, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return normalizeLocale(fallback)
	}
	for _, item := range prompt {
		if unicode.Is(unicode.Han, item) {
			return "zh-CN"
		}
	}
	return "en-US"
}

func metadataWithLocale(metadata map[string]any, locale string) map[string]any {
	items := map[string]any{}
	for key, value := range metadata {
		items[key] = value
	}
	items["locale"] = normalizeLocale(locale)
	return items
}

func localeFromInspectionMetadata(metadata map[string]any, fallback string) string {
	if metadata != nil {
		if value, ok := metadata["locale"].(string); ok && strings.TrimSpace(value) != "" {
			return normalizeLocale(value)
		}
	}
	return normalizeLocale(fallback)
}

func localizedScopeLabel(scope, locale string) string {
	switch strings.TrimSpace(scope) {
	case "cluster":
		return localize(locale, "集群", "cluster")
	case "namespace":
		return localize(locale, "命名空间", "namespace")
	default:
		return localize(locale, "平台", "platform")
	}
}
