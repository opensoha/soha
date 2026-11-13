package sohacli

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func kubernetesPodTerminalPath(cluster, namespace, name, id string) (string, error) {
	path, err := kubernetesPodExecutionPath(cluster, namespace, name)
	if err != nil {
		return "", err
	}
	path = strings.TrimSuffix(path, "/executions") + "/terminal-sessions"
	if id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id {
			return "", usageError{message: "provide one canonical native terminal session UUID"}
		}
		path += "/" + id
	}
	return path, nil
}

func (client APIClient) createPodTerminal(ctx context.Context, options podTerminalOptions, input contractresource.PodTerminalInput) (contractresource.PodTerminalCreated, error) {
	var out struct {
		Data contractresource.PodTerminalCreated `json:"data"`
	}
	input, err := input.Normalized()
	if err != nil {
		return out.Data, usageError{message: err.Error()}
	}
	path, err := kubernetesPodTerminalPath(options.Cluster, options.Namespace, options.Name, "")
	if err != nil {
		return out.Data, err
	}
	err = client.doJSON(ctx, http.MethodPost, kubernetesQuery(path, options.Namespace), client.Token, nil, input, &out)
	if err != nil {
		return contractresource.PodTerminalCreated{}, err
	}
	if err = validatePodTerminalLease(out.Data.Session, options); err != nil {
		return contractresource.PodTerminalCreated{}, err
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(out.Data.Ticket)
	if err != nil || len(data) != 32 || base64.RawURLEncoding.EncodeToString(data) != out.Data.Ticket || out.Data.Session.Status != "pending" || out.Data.Session.Target.UID != input.ExpectedUID || input.Container != "" && out.Data.Session.Target.Container != input.Container || out.Data.Session.Shell != input.Shell || out.Data.Session.DeadlineAt.Sub(out.Data.Session.CreatedAt) > time.Duration(input.TimeoutSeconds)*time.Second {
		return contractresource.PodTerminalCreated{}, fmt.Errorf("invalid native terminal authorization response")
	}
	return out.Data, nil
}

func (client APIClient) readOrClosePodTerminal(ctx context.Context, options podTerminalOptions, closeSession bool) (contractresource.PodTerminalLease, error) {
	var out struct {
		Data contractresource.PodTerminalLease `json:"data"`
	}
	path, err := kubernetesPodTerminalPath(options.Cluster, options.Namespace, options.Name, options.ID)
	if err != nil {
		return out.Data, err
	}
	method := http.MethodGet
	if closeSession {
		method = http.MethodDelete
	}
	if err = client.doJSON(ctx, method, kubernetesQuery(path, options.Namespace), client.Token, nil, nil, &out); err != nil {
		return out.Data, err
	}
	if err = validatePodTerminalLease(out.Data, options); err != nil {
		return contractresource.PodTerminalLease{}, err
	}
	if closeSession && !slices.Contains([]string{"closing", "closed", "expired"}, out.Data.Status) {
		return contractresource.PodTerminalLease{}, fmt.Errorf("terminal close was not acknowledged; query the original session")
	}
	return out.Data, nil
}

func validatePodTerminalLease(item contractresource.PodTerminalLease, options podTerminalOptions) error {
	if !matchesPodTerminalLease(item, options) {
		return fmt.Errorf("native terminal response does not match the requested session and target")
	}
	if !validPodTerminalLifetime(item) {
		return fmt.Errorf("invalid native terminal target or lifetime bounds")
	}
	if item.MaxInputBytes != contractresource.NativePodTerminalMaxInputBytes || item.MaxOutputBytes != contractresource.NativePodTerminalMaxOutputBytes || !slices.Contains([]string{"pending", "active", "closing", "closed", "expired"}, item.Status) {
		return fmt.Errorf("invalid native terminal capability or state")
	}
	if item.Status == "closed" || item.Status == "expired" {
		if item.ClosedAt == nil || item.ClosedAt.Before(item.CreatedAt) {
			return fmt.Errorf("terminal metadata requires a close time")
		}
	} else if item.ClosedAt != nil || item.Result != nil {
		return fmt.Errorf("active terminal cannot assert a final result")
	}
	if item.Result != nil && (item.Status != "closed" || item.Result.Validate() != nil) {
		return fmt.Errorf("invalid native terminal result")
	}
	return nil
}

func matchesPodTerminalLease(item contractresource.PodTerminalLease, options podTerminalOptions) bool {
	id, err := uuid.Parse(item.ID)
	return err == nil && id.String() == item.ID && (options.ID == "" || item.ID == options.ID) && item.ClusterID == options.Cluster && item.Target.Namespace == options.Namespace && item.Target.PodName == options.Name && !item.Target.CheckedAt.IsZero()
}

func validPodTerminalLifetime(item contractresource.PodTerminalLease) bool {
	input, err := (contractresource.PodTerminalInput{ExpectedUID: item.Target.UID, Container: item.Target.Container, Shell: item.Shell}).Normalized()
	return err == nil && input.Container != "" && input.Shell == item.Shell && !item.CreatedAt.IsZero() && item.DeadlineAt.After(item.CreatedAt) && item.DeadlineAt.Sub(item.CreatedAt) <= 900*time.Second && item.TicketExpiresAt.After(item.CreatedAt) && !item.TicketExpiresAt.After(item.DeadlineAt) && item.TicketExpiresAt.Sub(item.CreatedAt) <= time.Minute
}

func (client APIClient) dialPodTerminal(ctx context.Context, options podTerminalOptions, created contractresource.PodTerminalCreated) (*websocket.Conn, error) {
	options.ID = created.Session.ID
	path, err := kubernetesPodTerminalPath(options.Cluster, options.Namespace, options.Name, options.ID)
	if err != nil {
		return nil, err
	}
	return client.dialNativeTerminal(ctx, path, url.Values{"namespace": {options.Namespace}}.Encode(), created.Session.ID, created.Ticket)
}
