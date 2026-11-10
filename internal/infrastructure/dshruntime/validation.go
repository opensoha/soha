package dshruntime

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)
var toolPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var assetIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
var assetVersionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

func bounded(s string, min, max int) bool {
	n := utf8.RuneCountInString(s)
	return utf8.ValidString(s) && n >= min && n <= max
}
func safe(n, min int64) bool { return n >= min && n <= maxSafeInteger }
func validID(s string) bool  { return len(s) <= 128 && idPattern.MatchString(s) }
func validIdentity(id Identity) bool {
	return validID(id.RunID) && safe(id.Attempt, 1) && safe(id.Fence, 1)
}
func validSubmission(s Submission) bool {
	return validIdentity(s.Identity) && validID(s.SubmissionID) && hashPattern.MatchString(s.InputHash)
}
func validState(s string) bool {
	switch s {
	case "accepted", "running", "waiting_tool", "completed", "failed", "canceled", "unknown":
		return true
	}
	return false
}
func validReceipt(r Receipt) bool {
	return r.SchemaVersion == SchemaVersion && r.Kind == "receipt" && validSubmission(r.Submission) && validState(r.Status) && safe(r.Sequence, 0)
}
func validUsage(u *Usage) bool {
	return u != nil && safe(u.InputTokens, 0) && safe(u.OutputTokens, 0) && safe(u.TotalTokens, 0) && u.TotalTokens == u.InputTokens+u.OutputTokens
}
func validStatus(s Status) bool {
	if s.SchemaVersion != SchemaVersion || s.Kind != "status" || !validSubmission(s.Submission) || !validState(s.Status) || !safe(s.Sequence, 0) {
		return false
	}
	if !bounded(s.StopReason, 0, 256) || !bounded(s.AssistantText, 0, 131072) || len(s.AssistantText) > 131072 {
		return false
	}
	if s.UsageKnown {
		return validUsage(s.Usage)
	}
	return s.Usage == nil
}
func validHealth(h Health) bool {
	if h.SchemaVersion != SchemaVersion || h.Kind != "health" || h.ProtocolVersion != SchemaVersion || !bounded(h.RuntimeVersion, 1, 128) || len(h.Capabilities) > 64 {
		return false
	}
	seen := make(map[string]bool)
	for _, s := range h.Capabilities {
		if !bounded(s, 1, 128) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func validStart(s StartRequest) bool {
	if !validIdentity(s.Identity) || !validID(s.SessionID) || !validID(s.RequestID) || !hashPattern.MatchString(s.InputHash) {
		return false
	}
	if !bounded(s.OwnerKey, 1, 256) || !bounded(s.ContextRevision, 1, 128) || !bounded(s.ToolCatalogRevision, 1, 128) || !bounded(s.Model, 1, 256) {
		return false
	}
	if (s.Profile != "chat" && s.Profile != "heya") || !bounded(s.Prompt, 1, 131072) || !safe(s.MaxOutputTokens, 1) || s.MaxOutputTokens > 131072 {
		return false
	}
	deadline, err := time.Parse(time.RFC3339Nano, s.Deadline)
	if err != nil || !deadline.After(time.Now()) || !bounded(s.ToolGrant, 0, 8192) {
		return false
	}
	return validAssembly(s) && validContext(s.Context) && validTools(s.Tools, s.Profile)
}

func validAssembly(s StartRequest) bool {
	a := s.Assembly
	if a == nil {
		return true
	}
	if s.ToolGrant == "" || !assetIDPattern.MatchString(a.ProfileID) || !safe(a.Revision, 1) || a.Assets == nil || len(a.Assets) > 16 {
		return false
	}
	for _, asset := range a.Assets {
		if !assetIDPattern.MatchString(asset.ID) || !assetVersionPattern.MatchString(asset.Version) {
			return false
		}
		switch asset.Kind {
		case "mcp", "skill", "hook":
		default:
			return false
		}
	}
	return true
}
func validContext(messages []ContextMessage) bool {
	if len(messages) > 128 {
		return false
	}
	for _, m := range messages {
		if (m.Role != "user" && m.Role != "assistant") || !bounded(m.Content, 1, 131072) {
			return false
		}
	}
	return true
}
func validTools(tools []Tool, profile string) bool {
	if len(tools) > 64 {
		return false
	}
	seen := make(map[string]bool)
	for _, t := range tools {
		if !bounded(t.Name, 1, 128) || !toolPattern.MatchString(t.Name) || !bounded(t.Description, 0, 4096) || seen[t.Name] || (profile == "chat" && !t.ReadOnly) {
			return false
		}
		seen[t.Name] = true
		if !validToolSchema(t.InputSchema) {
			return false
		}
	}
	return true
}
func validToolSchema(data []byte) bool {
	if len(data) > 131072 {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := readValue(d, 0, true)
	if err != nil {
		return false
	}
	if _, err = d.Token(); err != io.EOF {
		return false
	}
	_, object := v.(map[string]any)
	return object && validJSONValue(v) && localReferences(v)
}
func validJSONValue(v any) bool {
	switch x := v.(type) {
	case string:
		return bounded(x, 0, 131072)
	case []any:
		if len(x) > 256 {
			return false
		}
		for _, e := range x {
			if !validJSONValue(e) {
				return false
			}
		}
	case map[string]any:
		if len(x) > 64 {
			return false
		}
		for k, e := range x {
			if !bounded(k, 0, 128) || !validJSONValue(e) {
				return false
			}
		}
	}
	return true
}
func localReferences(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if k == "$ref" || k == "$dynamicRef" {
				s, ok := e.(string)
				if !ok || !strings.HasPrefix(s, "#") {
					return false
				}
			}
			if !localReferences(e) {
				return false
			}
		}
	case []any:
		for _, e := range x {
			if !localReferences(e) {
				return false
			}
		}
	}
	return true
}
func validEvent(e Event, id Identity, after int64) bool {
	if e.SchemaVersion != SchemaVersion || e.Kind != "event" || e.Identity != id || !safe(e.Sequence, 1) || e.Sequence <= after {
		return false
	}
	switch e.Type {
	case "accepted", "started", "tool_started", "tool_completed", "completed", "failed", "canceled", "unknown":
	case "message_delta":
		if e.Data.Delta == "" {
			return false
		}
	case "hook_observed":
		if e.Data.CallID == "" || e.Data.ToolName == "" || !validHookOutcome(e.Data.Outcome) {
			return false
		}
	default:
		return false
	}
	return validEventData(e.Data)
}
func validHookOutcome(outcome string) bool {
	switch outcome {
	case "completed", "denied", "failed", "canceled", "unknown":
		return true
	}
	return false
}
func validEventData(d EventData) bool {
	if !bounded(d.Delta, 0, 16384) || len(d.Delta) > 16384 || !bounded(d.AssistantText, 0, 131072) || len(d.AssistantText) > 131072 {
		return false
	}
	if d.CallID != "" && !validID(d.CallID) {
		return false
	}
	if d.Status != "" && !validState(d.Status) {
		return false
	}
	if !bounded(d.ToolName, 0, 128) || !bounded(d.StopReason, 0, 256) || !bounded(d.ErrorCode, 0, 128) {
		return false
	}
	if d.UsageKnown != nil && *d.UsageKnown {
		return validUsage(d.Usage)
	}
	return d.Usage == nil
}
func validPage(p EventPage, id Identity, after int64) bool {
	if len(p.Items) > 256 || !safe(p.NextSequence, 0) {
		return false
	}
	last := after
	outputBytes := 0
	for _, e := range p.Items {
		if !validEvent(e, id, last) {
			return false
		}
		last = e.Sequence
		outputBytes += len(e.Data.Delta)
	}
	return outputBytes <= 131072 && p.NextSequence == last && !(len(p.Items) == 0 && p.HasMore)
}

// Token parsing rejects duplicate keys and nulls before typed decoding can erase them.
func readValue(d *json.Decoder, depth int, allowNull bool) (any, error) {
	if depth > 16 {
		return nil, ErrProtocol
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if t == nil && !allowNull {
		return nil, ErrProtocol
	}
	delim, compound := t.(json.Delim)
	if !compound {
		return t, nil
	}
	if delim == '{' {
		return readObject(d, depth, allowNull)
	}
	if delim != '[' {
		return nil, ErrProtocol
	}
	a := make([]any, 0)
	for d.More() {
		v, err := readValue(d, depth+1, allowNull)
		if err != nil {
			return nil, err
		}
		a = append(a, v)
	}
	end, err := d.Token()
	if err != nil || end != json.Delim(']') {
		return nil, ErrProtocol
	}
	return a, nil
}
func readObject(d *json.Decoder, depth int, allowNull bool) (any, error) {
	m := make(map[string]any)
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		k, ok := t.(string)
		if !ok {
			return nil, ErrProtocol
		}
		if _, exists := m[k]; exists {
			return nil, ErrProtocol
		}
		v, err := readValue(d, depth+1, allowNull)
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return nil, ErrProtocol
	}
	return m, nil
}
func required(m map[string]any, keys string) bool {
	for _, k := range strings.Fields(keys) {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
func allowed(m map[string]any, keys string) bool {
	set := make(map[string]bool)
	for _, key := range strings.Fields(keys) {
		set[key] = true
	}
	for key := range m {
		if !set[key] {
			return false
		}
	}
	return true
}
func decodeResponse(data []byte, out any) error {
	if !utf8.Valid(data) {
		return ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := readValue(d, 0, false)
	if err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF {
		return ErrProtocol
	}
	m, ok := v.(map[string]any)
	if !ok || !requiredResponse(m, out) {
		return ErrProtocol
	}
	if _, probe := out.(*runtimeMCPProbe); !probe && !validResponseFields(v) {
		return ErrProtocol
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(out)
}
func requiredResponse(m map[string]any, out any) bool {
	identity := "schemaVersion kind runId attempt fence"
	submission := identity + " submissionId inputHash status sequence"
	switch out.(type) {
	case *Health:
		keys := "schemaVersion kind ready protocolVersion runtimeVersion capabilities"
		return required(m, keys) && allowed(m, keys)
	case *Receipt:
		return required(m, submission) && allowed(m, submission)
	case *Status:
		return required(m, submission+" usageKnown") && allowed(m, submission+" usageKnown usage stopReason assistantText")
	case *EventPage:
		if !required(m, "items nextSequence hasMore") || !allowed(m, "items nextSequence hasMore") {
			return false
		}
		items, ok := m["items"].([]any)
		if !ok {
			return false
		}
		for _, v := range items {
			event, ok := v.(map[string]any)
			if !ok || !required(event, identity+" sequence type data") || !allowed(event, identity+" sequence type data") {
				return false
			}
		}
		return true
	case *runtimeAssetList:
		items, ok := m["items"].([]any)
		if !ok || !allowed(m, "items") {
			return false
		}
		for _, value := range items {
			asset, ok := value.(map[string]any)
			if !ok || !required(asset, "kind id version") || !allowed(asset, "kind id version transport executionPosition") {
				return false
			}
		}
		return true
	case *runtimeMCPProbe:
		return required(m, "status capabilities") && allowed(m, "status capabilities")
	}
	return false
}
func validResponseFields(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if !validOptionalField(k, e) || !validResponseFields(e) {
				return false
			}
		}
	case []any:
		for _, e := range x {
			if !validResponseFields(e) {
				return false
			}
		}
	}
	return true
}
func validOptionalField(key string, value any) bool {
	if key == "status" {
		s, ok := value.(string)
		return ok && validState(s)
	}
	limits := map[string]int{"delta": 16384, "callId": 128, "toolName": 128, "stopReason": 256, "errorCode": 128}
	if max, ok := limits[key]; ok {
		s, ok := value.(string)
		return ok && bounded(s, 1, max)
	}
	if key == "usage" {
		m, ok := value.(map[string]any)
		return ok && required(m, "inputTokens outputTokens totalTokens") && allowed(m, "inputTokens outputTokens totalTokens")
	}
	if key == "data" {
		m, ok := value.(map[string]any)
		return ok && allowed(m, "delta assistantText callId toolName status stopReason errorCode usageKnown usage outcome")
	}
	return true
}
