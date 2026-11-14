package sohaapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMonitoringDTOsKeepNullAndZeroDistinct(t *testing.T) {
	var nullPoint, zeroPoint ResourceMonitoringPoint
	if err := json.Unmarshal([]byte(`{"value":null}`), &nullPoint); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"value":0}`), &zeroPoint); err != nil {
		t.Fatal(err)
	}
	if nullPoint.Value != nil || zeroPoint.Value == nil || *zeroPoint.Value != 0 {
		t.Fatal("null sample became zero")
	}
	var quantity ResourceMonitoringQuantity
	if err := json.Unmarshal([]byte(`{"cpuCores":null,"memoryBytes":0,"podSlots":null}`), &quantity); err != nil {
		t.Fatal(err)
	}
	if quantity.CPUCores != nil || quantity.MemoryBytes == nil || *quantity.MemoryBytes != 0 {
		t.Fatal("nullable capacity lost")
	}
	zero := int64(0)
	edge := KubernetesTopologyGraphEdge{Weight: &zero}
	raw, err := json.Marshal(edge)
	if err != nil || !strings.Contains(string(raw), `"weight":0`) {
		t.Fatalf("zero weight omitted: %s %v", raw, err)
	}
}
func TestPublishedCacheEnumNamesRemainSourceCompatible(t *testing.T) {
	values := []string{
		string(LLMCallLogCacheStatusBypass), string(LLMCallLogCacheStatusHit), string(LLMCallLogCacheStatusMiss), string(LLMCallLogCacheStatusStaleHit), string(LLMCallLogCacheStatusWrite), string(LLMCallLogCacheStatusWriteSkipped),
		string(CreateAIGatewayAnthropicMessageParamsXSohaCacheModeBypass),
		string(CreateAIGatewayCohereRerankParamsXSohaCacheModeBypass),
		string(CreateAIGatewayGeminiGenerateContentParamsXSohaCacheModeBypass),
		string(CreateAIGatewayGeminiInteractionsParamsXSohaCacheModeBypass),
		string(CreateAIGatewayGeminiStreamGenerateContentParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIAudioSpeechParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIAudioTranscriptionParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIAudioTranslationParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIChatCompletionParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderAudioSpeechParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderAudioTranscriptionParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderAudioTranslationParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderChatCompletionParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderEmbeddingParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderImageEditParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderImageGenerationParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderImageVariationParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAICompatibleProviderResponseParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIEmbeddingParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIImageEditParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIImageGenerationParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIImageVariationParamsXSohaCacheModeBypass),
		string(CreateAIGatewayOpenAIResponseParamsXSohaCacheModeBypass),
		string(SohaCacheModeHeaderBypass),
	}
	for _, value := range values {
		if value == "" {
			t.Fatal("empty published enum")
		}
	}
}
