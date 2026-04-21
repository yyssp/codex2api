package proxy

import (
	"encoding/json"
	"strings"
)

// SupportedModels 支持的模型列表（全局共享）
var SupportedModels = []string{
	"gpt-5.4", "gpt-5.4-mini", "gpt-5", "gpt-5-codex", "gpt-5-codex-mini",
	"gpt-5.1", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5.1-codex-max",
	"gpt-5.2", "gpt-5.2-codex", "gpt-5.3-codex",
}

var defaultAnthropicModelMap = map[string]string{
	"claude-opus-4-6":            "gpt-5.4",
	"claude-opus-4-6-20250610":   "gpt-5.4",
	"claude-haiku-4-5-20251001":  "gpt-5.4-mini",
	"claude-haiku-4-5":           "gpt-5.4-mini",
	"claude-sonnet-4-6":          "gpt-5.3-codex",
	"claude-sonnet-4-5-20250929": "gpt-5.2-codex",
	"claude-opus-4-5-20251101":   "gpt-5.3-codex",
	"claude-sonnet-4-5-20250514": "gpt-5.4",
	"claude-sonnet-4-5":          "gpt-5.4",
	"claude-sonnet-4.5":          "gpt-5.4",
	"claude-sonnet-4-20250514":   "gpt-5.4",
	"claude-sonnet-4":            "gpt-5.4",
	"claude-opus-4-20250514":     "gpt-5.4",
	"claude-opus-4":              "gpt-5.4",
	"claude-3-5-sonnet-20241022": "gpt-5.4",
	"claude-3-5-haiku-20241022":  "gpt-5.4-mini",
}

type AdminModelCatalog struct {
	Models                    []string          `json:"models"`
	DefaultAnthropicMapping   map[string]string `json:"default_anthropic_mapping"`
	EffectiveAnthropicMapping map[string]string `json:"effective_anthropic_mapping"`
}

func cloneStringSlice(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneModelMapping(src map[string]string) map[string]string {
	cloned := make(map[string]string, len(src))
	for key, value := range src {
		cloned[key] = value
	}
	return cloned
}

func DefaultAnthropicModelMapping() map[string]string {
	return cloneModelMapping(defaultAnthropicModelMap)
}

func EffectiveAnthropicModelMapping(dynamicMappingJSON string) map[string]string {
	effective := DefaultAnthropicModelMapping()
	if strings.TrimSpace(dynamicMappingJSON) == "" || dynamicMappingJSON == "{}" {
		return effective
	}

	var dynamicMap map[string]string
	if err := json.Unmarshal([]byte(dynamicMappingJSON), &dynamicMap); err != nil {
		return effective
	}
	for key, value := range dynamicMap {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		effective[key] = value
	}
	return effective
}

func BuildAdminModelCatalog(dynamicMappingJSON string) AdminModelCatalog {
	return AdminModelCatalog{
		Models:                    cloneStringSlice(SupportedModels),
		DefaultAnthropicMapping:   DefaultAnthropicModelMapping(),
		EffectiveAnthropicMapping: EffectiveAnthropicModelMapping(dynamicMappingJSON),
	}
}

func ResolveAnthropicModel(model string, dynamicMappingJSON string) string {
	if mapped, ok := EffectiveAnthropicModelMapping(dynamicMappingJSON)[model]; ok {
		return mapped
	}

	for _, supported := range SupportedModels {
		if model == supported {
			return model
		}
	}

	lower := strings.ToLower(model)
	if strings.Contains(lower, "haiku") {
		return "gpt-5.4-mini"
	}
	if strings.Contains(lower, "claude") {
		return "gpt-5.4"
	}

	if len(SupportedModels) > 0 {
		return SupportedModels[0]
	}
	return "gpt-5.4"
}
