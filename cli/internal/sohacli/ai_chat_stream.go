package sohacli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const aiStreamFrameLimit = 2 << 20
const aiStreamTotalLimit = 32 << 20

// Submission sequences belong to this SSE connection, never to the durable run cursor.
type aiSubmissionReceipt struct {
	SessionID string `json:"sessionId"`
	RequestID string `json:"requestId"`
	RunID     string `json:"runId,omitempty"`
	Status    string `json:"status"`
}

type aiStreamOutput struct {
	Source  string                        `json:"source"`
	Event   *sohaapi.WorkbenchStreamEvent `json:"event,omitempty"`
	Receipt *aiSubmissionReceipt          `json:"receipt,omitempty"`
}

func runAIChatStream(ctx context.Context, args []string, rt Runtime) error {
	leading, rest := extractLeadingPositionals(args, 1)
	fs := newRuntimeFlagSet("ai chat messages stream", rest, rt)
	profile := fs.String("profile", "", "profile name")
	file := fs.String("input", "", "typed JSON file, or - for stdin")
	inline := fs.String("input-json", "", "typed JSON request")
	key := fs.String("request-id", "", "stable original turn ID; conflicts with a different input requestId")
	output := fs.String("output", "ndjson", "output format: ndjson")
	wait := fs.Duration("stream-timeout", 10*time.Minute, "maximum stream duration; expiry leaves the outcome unknown")
	yes := fs.Bool("yes", false, "confirm sending the reviewed turn")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	id, err := aiWorkbenchID(append(leading, fs.Args()...), true)
	if err != nil {
		return err
	}
	if *output != "ndjson" {
		return usageError{message: "streaming messages require --output ndjson"}
	}
	if *wait < time.Second || *wait > 24*time.Hour {
		return usageError{message: "stream-timeout must be between 1s and 24h"}
	}
	var input sohaapi.WorkbenchSendMessageStreamRequest
	if err := readWorkbenchInput(rt, *file, *inline, &input); err != nil {
		return err
	}
	if err := prepareAIStreamInput(&input, *key); err != nil {
		return err
	}
	client, err := loadComputeClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	if err := confirmWorkbenchAction(rt, *yes, "Send the reviewed AI turn to session "+terminalLabel(id)+"?"); err != nil {
		return err
	}
	if rt.StreamingContext != nil {
		var stop context.CancelFunc
		ctx, stop = rt.StreamingContext(ctx)
		defer stop()
	}
	ctx, stop := context.WithTimeout(ctx, *wait)
	defer stop()
	return client.streamAIChat(ctx, id, input, rt.Out)
}

func prepareAIStreamInput(input *sohaapi.WorkbenchSendMessageStreamRequest, key string) error {
	if key != "" && input.RequestID != "" && key != input.RequestID {
		return usageError{message: "--request-id conflicts with input requestId"}
	}
	if key != "" {
		input.RequestID = key
	}
	if !utf8.ValidString(input.RequestID) || utf8.RuneCountInString(input.RequestID) > 128 || strings.TrimSpace(input.RequestID) == "" {
		return usageError{message: "stream requires a stable requestId of 1 to 128 characters"}
	}
	if strings.TrimSpace(input.Content) == "" {
		return usageError{message: "stream requires nonempty content"}
	}
	return nil
}

func (c APIClient) streamAIChat(ctx context.Context, session string, input sohaapi.WorkbenchSendMessageStreamRequest, out io.Writer) error {
	return c.receiveAIChat(ctx, session, input, func(update aiStreamOutput) error {
		return writeAINdjson(out, update, nil)
	})
}

// Both terminal and NDJSON consumers receive the same validated, redacted frames.
func (c APIClient) receiveAIChat(ctx context.Context, session string, input sohaapi.WorkbenchSendMessageStreamRequest, emit func(aiStreamOutput) error) error {
	if emit == nil {
		return fmt.Errorf("AI stream requires an event consumer")
	}
	response, token, err := c.openAIChatStream(ctx, session, input)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	receipt := aiSubmissionReceipt{SessionID: session, RequestID: input.RequestID}
	// IDs are escaped only for the HTTP path; compare frames against the original value.
	receipt.SessionID, _ = url.PathUnescape(session)
	secrets := append(c.aiStreamSecrets(), token)
	state := aiStreamState{receipt: receipt, emit: emit, secrets: secrets}
	err = readAIEventStream(response.Body, state.accept)
	if ctx.Err() != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return aiStreamUnknownError()
		}
		return ctx.Err()
	}
	if err != nil {
		return aiStreamUnknownError()
	}
	return state.finish()
}

func aiStreamUnknownError() error {
	return fmt.Errorf("AI stream outcome unknown; do not resend automatically; preserve the original session and request ID, inspect request or session messages")
}

func (c APIClient) openAIChatStream(ctx context.Context, session string, input sohaapi.WorkbenchSendMessageStreamRequest) (*http.Response, string, error) {
	base := strings.TrimRight(strings.TrimSpace(c.ServerURL), "/")
	if base == "" || !sameServerURL(base, base) {
		return nil, "", fmt.Errorf("invalid_server_url: API URL must not contain credentials, query or fragment")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, "", fmt.Errorf("cannot encode AI stream request")
	}
	token := c.effectiveToken(c.Token)
	response, err := c.openAIChatStreamOnce(ctx, base, session, raw, token)
	var rejected *httpStatusError
	if errors.As(err, &rejected) && rejected.StatusCode == http.StatusUnauthorized && rejected.AuthRejected && token != "" && c.canRefresh() {
		token, err = c.refreshAfterUnauthorized(ctx, token)
		if err == nil {
			response, err = c.openAIChatStreamOnce(ctx, base, session, raw, token)
		}
	}
	return response, token, err
}

func (c APIClient) openAIChatStreamOnce(ctx context.Context, base, session string, raw []byte, token string) (*http.Response, error) {
	path := copilotPath + "/sessions/" + session + "/messages/stream"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("cannot create AI stream request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := c.aiChatHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, aiStreamUnknownError()
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, aiStreamFrameLimit))
		return nil, &httpStatusError{Method: http.MethodPost, Path: path, Status: resp.Status, StatusCode: resp.StatusCode,
			Message: safeAPIErrorMessage(resp.Status, body, raw, token, c.RefreshToken), Code: responseErrorCode(body),
			AuthRejected: strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`)}
	}
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || media != "text/event-stream" {
		_ = resp.Body.Close()
		return nil, aiStreamUnknownError()
	}
	return resp, nil
}

func (c APIClient) aiChatHTTPClient() *http.Client {
	client := c.httpClient()
	// Ordinary JSON keeps its 30s default. Streaming uses the command's bounded
	// context, while an explicit --timeout or injected client timeout still wins.
	if c.Timeout <= 0 && (c.Client == nil || c.Client.Timeout <= 0) {
		client.Timeout = 0
	}
	// A 307/308 redirect can replay this POST, even at the same origin.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

// SSE dispatch requires a blank line. An unterminated last frame is not a receipt.
func readAIEventStream(reader io.Reader, accept func([]byte) (bool, error)) error {
	limited := &io.LimitedReader{R: reader, N: aiStreamTotalLimit + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), aiStreamFrameLimit+1)
	var data []byte
	for scanner.Scan() {
		if limited.N <= 0 {
			return fmt.Errorf("AI stream exceeds limit")
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			if len(data) == 0 {
				continue
			}
			done, err := accept(bytes.TrimSuffix(data, []byte{'\n'}))
			if err != nil || done {
				return err
			}
			data = nil
			continue
		}
		field, value, _ := bytes.Cut(line, []byte{':'})
		if !bytes.Equal(field, []byte("data")) {
			continue
		}
		value = bytes.TrimPrefix(value, []byte{' '})
		if len(data)+len(value)+1 > aiStreamFrameLimit {
			return fmt.Errorf("AI stream frame exceeds limit")
		}
		data = append(append(data, value...), '\n')
	}
	if scanner.Err() != nil || limited.N <= 0 || len(data) != 0 {
		return fmt.Errorf("AI stream incomplete or exceeds limit")
	}
	return nil
}

type aiStreamState struct {
	receipt  aiSubmissionReceipt
	emit     func(aiStreamOutput) error
	secrets  []string
	sequence int
	hadError bool
}

func (s *aiStreamState) accept(raw []byte) (bool, error) {
	if !utf8.Valid(raw) {
		return false, fmt.Errorf("invalid AI event encoding")
	}
	var event sohaapi.WorkbenchStreamEvent
	var meta struct {
		sohaapi.WorkbenchStreamEventBase
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &event) != nil || json.Unmarshal(raw, &meta) != nil || meta.Type == "" || meta.ID == "" || meta.CreatedAt.IsZero() || meta.SessionID != s.receipt.SessionID || meta.Sequence <= s.sequence {
		return false, fmt.Errorf("invalid AI event identity or sequence")
	}
	s.sequence = meta.Sequence
	if meta.Type == "error" {
		s.hadError = true
	}
	if meta.Type == "agent.status" {
		status, err := event.AsWorkbenchAgentStatusEvent()
		if err != nil || s.applyStatus(status) != nil {
			return false, fmt.Errorf("invalid AI status event")
		}
	}
	if err := s.publish(aiStreamOutput{Source: "submission", Event: &event}); err != nil {
		return false, err
	}
	return s.receipt.Status == "succeeded" || s.receipt.Status == "failed" || s.receipt.Status == "cancelled", nil
}

func (s *aiStreamState) applyStatus(event sohaapi.WorkbenchAgentStatusEvent) error {
	if event.RunID != "" {
		if _, err := aiWorkbenchID([]string{event.RunID}, true); err != nil || s.receipt.RunID != "" && s.receipt.RunID != event.RunID {
			return fmt.Errorf("invalid AI run identity")
		}
		s.receipt.RunID = event.RunID
	}
	switch string(event.Status) {
	case "queued", "running":
		s.receipt.Status = "accepted"
	case "succeeded", "failed", "cancelled":
		s.receipt.Status = string(event.Status)
	default:
		return fmt.Errorf("unknown AI status")
	}
	return nil
}

func (s *aiStreamState) finish() error {
	if s.receipt.Status == "" || s.receipt.Status == "accepted" && (s.receipt.RunID == "" || s.hadError) {
		return aiStreamUnknownError()
	}
	if err := s.publish(aiStreamOutput{Source: "submission", Receipt: &s.receipt}); err != nil {
		return err
	}
	if s.receipt.Status == "failed" || s.receipt.Status == "cancelled" {
		return fmt.Errorf("AI turn ended with status %s; inspect the original session or run", s.receipt.Status)
	}
	return nil
}

func (s *aiStreamState) publish(update aiStreamOutput) error {
	raw, err := json.Marshal(redactAIStreamSecrets(sanitizeCLIJSONValue(update), s.secrets))
	if err != nil {
		return fmt.Errorf("cannot encode AI stream event")
	}
	var safe aiStreamOutput
	if err := json.Unmarshal(raw, &safe); err != nil {
		return fmt.Errorf("cannot project AI stream event")
	}
	return s.emit(safe)
}

func writeAINdjson(out io.Writer, value any, secrets []string) error {
	raw, err := json.Marshal(redactAIStreamSecrets(sanitizeCLIJSONValue(value), secrets))
	if err != nil {
		return fmt.Errorf("cannot encode AI stream output")
	}
	raw = append(raw, '\n')
	_, err = out.Write(raw)
	return err
}

func redactAIStreamSecrets(value any, secrets []string) any {
	switch value := value.(type) {
	case string:
		return redactAIStreamText(value, secrets)
	case map[string]any:
		redacted := make(map[string]any, len(value))
		for key, child := range value {
			key = redactAIStreamText(key, secrets)
			redacted[key] = redactAIStreamSecrets(child, secrets)
		}
		return redacted
	case []any:
		for i, child := range value {
			value[i] = redactAIStreamSecrets(child, secrets)
		}
	}
	return value
}

func redactAIStreamText(value string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}
