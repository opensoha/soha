package copilot

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
	"go.yaml.in/yaml/v3"
)

func chatArtifactPreview(run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	title := strings.TrimSpace(redaction.Text(stringValue(input["title"])))
	rawContent, _ := input["content"].(string)
	content := redaction.Text(rawContent)
	kind, format := stringValue(input["artifactKind"]), stringValue(input["format"])
	if title == "" || utf8.RuneCountInString(title) > 120 || strings.TrimSpace(content) == "" || !utf8.ValidString(content) || len(content) > 8192 || len(run.AnalysisArtifacts) >= 4 ||
		!slices.Contains([]string{"report", "configuration_preview", "resource_table"}, kind) || !slices.Contains([]string{"markdown", "yaml", "json", "text"}, format) {
		return nil, fmt.Errorf("%w: static artifact requires a title, supported kind/format and up to 8 KiB of content; at most four drafts per run", apperrors.ErrInvalidArgument)
	}
	if format == "json" && !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("%w: invalid JSON draft", apperrors.ErrInvalidArgument)
	}
	if format == "yaml" {
		var document any
		if err := yaml.Unmarshal([]byte(content), &document); err != nil {
			return nil, fmt.Errorf("%w: invalid YAML draft", apperrors.ErrInvalidArgument)
		}
	}
	baseline, err := chatArtifactBaseline(run, stringValue(input["baselineCitationId"]))
	if err != nil {
		return nil, err
	}
	artifactID := "artifact:" + uuid.NewString()
	artifact := domaincopilot.AnalysisArtifact{
		Kind: kind, RunID: artifactID, Title: title,
		Summary:            localize(stringValue(run.Input["locale"]), "对话生成的静态草稿。", "Static draft generated in this conversation."),
		DataSourceSnapshot: map[string]any{"format": format, "content": content, "baseline": baseline, "baselineCitationId": stringValue(input["baselineCitationId"]), "state": "draft", "agentRunId": run.ID, "sessionId": run.SessionID},
	}
	return map[string]any{"result": "success", "artifactId": artifactID, "title": title, "state": "draft", "_artifact": artifact}, nil
}

func chatArtifactBaseline(run domaincopilot.AgentRun, citationID string) (string, error) {
	if citationID == "" {
		return "", nil
	}
	if len(agentPrincipalStringList(mapValue(run.Input["contextSnapshot"])["truncations"])) > 0 {
		return "", fmt.Errorf("%w: clipped context cannot be a configuration baseline", apperrors.ErrInvalidArgument)
	}
	data, err := json.Marshal(run.Input["_sohaEvidence"])
	if err != nil {
		return "", err
	}
	var evidence []domaincopilot.ContextEvidence
	if err := json.Unmarshal(data, &evidence); err != nil {
		return "", err
	}
	for _, item := range evidence {
		if item.CitationID == citationID {
			return item.Content, nil
		}
	}
	return "", fmt.Errorf("%w: baseline must reference evidence supplied in this turn", apperrors.ErrAccessDenied)
}
