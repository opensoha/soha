package sohacli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"github.com/opensoha/soha-contracts/resource"
)

func dockerTerminalPath(project, id string) (string, error) {
	if !kubernetesSegment(project) || !protocol.SafeText(project, 128) {
		return "", usageError{message: "provide one valid Docker project ID"}
	}
	path := "/api/v1/docker/projects/" + url.PathEscape(project) + "/terminal-sessions"
	if id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil || parsed.String() != id {
			return "", usageError{message: "provide one canonical terminal session UUID"}
		}
		path += "/" + id
	}
	return path, nil
}

func (client APIClient) nativeDockerTerminalJSON(ctx context.Context, method, path string, input, out any) error {
	var raw json.RawMessage
	if err := client.doJSON(ctx, method, path, client.Token, nil, input, &raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 16<<10 || !utf8.Valid(raw) || decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("invalid Docker native terminal response")
	}
	return nil
}

func (client APIClient) createDockerTerminal(ctx context.Context, opt dockerTerminalOptions, input protocol.TerminalInput) (protocol.TerminalCreated, error) {
	var out struct {
		Data protocol.TerminalCreated `json:"data"`
	}
	input, err := input.Normalized()
	if err != nil {
		return out.Data, usageError{message: err.Error()}
	}
	path, err := dockerTerminalPath(opt.Project, "")
	if err != nil {
		return out.Data, err
	}
	if err := client.nativeDockerTerminalJSON(ctx, http.MethodPost, path, input, &out); err != nil {
		return protocol.TerminalCreated{}, err
	}
	item := out.Data.Session
	if err := validateDockerTerminalLease(item, opt); err != nil {
		return protocol.TerminalCreated{}, err
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(out.Data.Ticket)
	if err != nil || len(data) != 32 || base64.RawURLEncoding.EncodeToString(data) != out.Data.Ticket || item.Status != "pending" || !item.TicketExpiresAt.After(time.Now()) || item.Target.ContainerID != input.ExpectedContainerID || item.Target.ServiceName != input.ServiceName || item.Shell != input.Shell || item.DeadlineAt.Sub(item.CreatedAt) > time.Duration(input.TimeoutSeconds)*time.Second {
		return protocol.TerminalCreated{}, fmt.Errorf("invalid Docker native terminal authorization response")
	}
	return out.Data, nil
}

func (client APIClient) readOrCloseDockerTerminal(ctx context.Context, opt dockerTerminalOptions, closeSession bool) (protocol.TerminalLease, error) {
	var out struct {
		Data protocol.TerminalLease `json:"data"`
	}
	path, err := dockerTerminalPath(opt.Project, opt.ID)
	if err != nil {
		return out.Data, err
	}
	method := http.MethodGet
	if closeSession {
		method = http.MethodDelete
	}
	if err := client.nativeDockerTerminalJSON(ctx, method, path, nil, &out); err != nil {
		return protocol.TerminalLease{}, err
	}
	if err := validateDockerTerminalLease(out.Data, opt); err != nil {
		return protocol.TerminalLease{}, err
	}
	if closeSession && !slices.Contains([]string{"closing", "closed", "expired"}, out.Data.Status) {
		return protocol.TerminalLease{}, fmt.Errorf("terminal close was not acknowledged; query the original session")
	}
	return out.Data, nil
}

func validateDockerTerminalLease(item protocol.TerminalLease, opt dockerTerminalOptions) error {
	if !matchesDockerTerminalLease(item, opt) {
		return fmt.Errorf("docker native terminal response differs from the original session or target")
	}

	if !validDockerTerminalLifetime(item) {
		return fmt.Errorf("invalid Docker native terminal lifetime")
	}

	if item.MaxInputBytes != resource.NativePodTerminalMaxInputBytes || item.MaxOutputBytes != resource.NativePodTerminalMaxOutputBytes || !slices.Contains([]string{"pending", "active", "closing", "closed", "expired"}, item.Status) {
		return fmt.Errorf("invalid Docker native terminal capability or state")
	}
	if item.Status == "closed" || item.Status == "expired" {
		if item.ClosedAt == nil || item.ClosedAt.Before(item.CreatedAt) {
			return fmt.Errorf("terminal metadata requires a close time")
		}
	} else if item.ClosedAt != nil || item.Result != nil {
		return fmt.Errorf("active terminal cannot assert a final result")
	}
	if item.Result != nil && (item.Status != "closed" || item.Result.Validate() != nil) {
		return fmt.Errorf("invalid Docker native terminal result")
	}
	return nil
}

func matchesDockerTerminalLease(item protocol.TerminalLease, opt dockerTerminalOptions) bool {
	id, err := uuid.Parse(item.ID)
	return err == nil && id != uuid.Nil && id.String() == item.ID && (opt.ID == "" || item.ID == opt.ID) && item.Protocol == protocol.TerminalProtocol && item.ProjectID == opt.Project && item.Target.Validate() == nil
}

func validDockerTerminalLifetime(item protocol.TerminalLease) bool {
	input, err := (protocol.TerminalInput{ExpectedContainerID: item.Target.ContainerID, ServiceName: item.Target.ServiceName, Shell: item.Shell}).Normalized()
	return err == nil && input.Shell == item.Shell && !item.CreatedAt.IsZero() && item.DeadlineAt.After(item.CreatedAt) && item.DeadlineAt.Sub(item.CreatedAt) <= 900*time.Second && item.TicketExpiresAt.After(item.CreatedAt) && !item.TicketExpiresAt.After(item.DeadlineAt) && item.TicketExpiresAt.Sub(item.CreatedAt) <= time.Minute
}

func (client APIClient) dialDockerTerminal(ctx context.Context, opt dockerTerminalOptions, created protocol.TerminalCreated) (*websocket.Conn, error) {
	path, err := dockerTerminalPath(opt.Project, created.Session.ID)
	if err != nil {
		return nil, err
	}
	return client.dialNativeTerminal(ctx, path, "", created.Session.ID, created.Ticket)
}
