package copilot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
)

func chatRunSources(run domaincopilot.AgentRun) []domaincopilot.WorkbenchSource {
	var sources []domaincopilot.WorkbenchSource
	seen := make(map[string]bool)
	add := func(id, title, summary, uri string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		parsed, err := url.Parse(uri)
		if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			uri = ""
		}
		text := []rune(summary)
		if len(text) > 1600 {
			summary = string(text[:1600]) + "…"
		}
		sources = append(sources, domaincopilot.WorkbenchSource{ID: id, Kind: "document", Title: firstNonEmpty(title, id), Summary: summary, URL: uri})
	}
	var citations []domainknowledge.Citation
	data, _ := json.Marshal(mapValue(run.Input["contextSnapshot"])["citations"])
	_ = json.Unmarshal(data, &citations)
	var evidence []domaincopilot.ContextEvidence
	data, _ = json.Marshal(run.Input["_sohaEvidence"])
	_ = json.Unmarshal(data, &evidence)
	for _, citation := range citations {
		summary := ""
		for _, item := range evidence {
			if item.CitationID == citation.ID {
				summary = item.Content
				break
			}
		}
		add(citation.ID, citation.DocumentTitle, summary, citation.URI)
	}
	for _, execution := range run.ToolExecutions {
		for _, source := range chatDelegatedSources(execution) {
			add(source.ID, source.Title, source.Summary, source.URL)
		}

		if execution.Status != "success" || stringValue(execution.Output["result"]) != "success" {
			continue
		}
		content := stringValue(execution.Output["content"])
		add(stringValue(execution.Output["citationId"]), execution.ToolName, content, "")
		if execution.ToolName == "knowledge.search" {
			var result domainknowledge.SearchResult
			if json.Unmarshal([]byte(content), &result) == nil {
				for _, hit := range result.Hits {
					add(hit.Citation.ID, firstNonEmpty(hit.Title, hit.Citation.DocumentTitle), hit.Content, hit.Citation.URI)
				}
			}
		}
	}
	return sources
}

func chatUsedContextSnapshot(run domaincopilot.AgentRun, sources []domaincopilot.WorkbenchSource) map[string]any {
	initial := mapValue(run.Input["contextSnapshot"])
	if len(run.ToolExecutions) == 0 {
		return initial
	}
	snapshot := make(map[string]any, len(initial)+4)
	for key, value := range initial {
		snapshot[key] = value
	}
	var usage domaincopilot.ContextBudgetUsage
	data, _ := json.Marshal(initial["budgetUsage"])
	_ = json.Unmarshal(data, &usage)
	var hashes []string
	for _, execution := range run.ToolExecutions {
		if content := stringValue(execution.Output["content"]); strings.TrimSpace(content) != "" {
			usage.EvidenceTokens += intCondition(execution.Output["estimatedTokens"])
			usage.EvidenceItems++
			hash := sha256.Sum256([]byte(content))
			hashes = append(hashes, hex.EncodeToString(hash[:]))
		}
	}
	snapshot["inputSnapshotId"], snapshot["id"] = initial["id"], run.ID+":used"
	snapshot["budgetUsage"], snapshot["toolSources"], snapshot["toolContentHashes"] = usage, sources, hashes
	snapshot["usageProvenance"] = "estimated_characters"
	delete(snapshot, "contentHash")
	data, _ = json.Marshal(snapshot)
	hash := sha256.Sum256(data)
	snapshot["contentHash"] = hex.EncodeToString(hash[:])
	return snapshot
}

func chatDelegatedSources(execution domaincopilot.ToolExecution) []domaincopilot.WorkbenchSource {
	if execution.Status != "success" || execution.ToolName != "agent.delegate" {
		return nil
	}
	var sources []domaincopilot.WorkbenchSource
	data, _ := json.Marshal(execution.Output["sources"])
	if json.Unmarshal(data, &sources) != nil {
		return nil
	}
	return sources
}
