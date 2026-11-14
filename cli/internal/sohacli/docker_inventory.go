package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// These client projections deliberately omit server configuration and extensions.
type dockerInventoryItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (item dockerInventoryItem) inventoryID() string { return item.ID }

type dockerHostInventory struct {
	dockerInventoryItem
	Environment  string `json:"environment,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

type dockerProjectInventory struct {
	dockerInventoryItem
	HostID      string `json:"hostId"`
	Slug        string `json:"slug,omitempty"`
	Environment string `json:"environment,omitempty"`
	SourceKind  string `json:"sourceKind,omitempty"`
}

type dockerServiceInventory struct {
	dockerInventoryItem
	HostID    string `json:"hostId"`
	ProjectID string `json:"projectId"`
}

type dockerInventoryRow interface{ inventoryID() string }

type dockerInventoryPage[T dockerInventoryRow] struct {
	Items               []T   `json:"items"`
	Total               int64 `json:"total"`
	Page                int   `json:"page"`
	PageSize            int   `json:"pageSize"`
	ConfigurationHidden bool  `json:"configurationHidden"`
}

func runDockerInventory(ctx context.Context, resource string, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("docker "+resource+" list", args, rt)
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "json", "output format: json or yaml")
	page := fs.Int("page", 1, "page number: 1..1000000")
	size := fs.Int("page-size", 50, "items per page: 1..100; one request, no automatic pagination")
	filters := map[string]*string{}
	for _, name := range []string{"search", "status", "environment", "architecture", "host-id", "project-id", "source-kind"} {
		filters[name] = fs.String(name, "", "server-side "+name+" filter")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *page < 1 || *page > 1000000 || *size < 1 || *size > 100 {
		return usageError{message: "Docker list accepts no resource ID; page must be 1..1000000 and page-size 1..100"}
	}
	allowed := []string{"profile", "output", "page", "page-size", "search", "status"}
	switch resource {
	case "hosts":
		allowed = append(allowed, "environment", "architecture")
	case "projects":
		allowed = append(allowed, "host-id", "environment", "source-kind")
	case "services":
		allowed = append(allowed, "host-id", "project-id")
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return err
	}
	query, err := dockerInventoryQuery(filters, *page, *size)
	if err != nil {
		return err
	}
	format, client, err := computeOutputClient(ctx, rt, *profile, *output)
	if err != nil {
		return err
	}
	switch resource {
	case "hosts":
		return readDockerInventory[dockerHostInventory](ctx, rt, client, format, resource, query, *page, *size)
	case "projects":
		return readDockerInventory[dockerProjectInventory](ctx, rt, client, format, resource, query, *page, *size)
	case "services":
		return readDockerInventory[dockerServiceInventory](ctx, rt, client, format, resource, query, *page, *size)
	default:
		return usageError{message: "unsupported Docker inventory"}
	}
}

func dockerInventoryQuery(filters map[string]*string, page, size int) (url.Values, error) {
	query := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(size)}}
	keys := map[string]string{"host-id": "hostId", "project-id": "projectId", "source-kind": "sourceKind"}
	for name, pointer := range filters {
		value := strings.TrimSpace(*pointer)
		if value == "" {
			continue
		}
		if len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") || (name == "host-id" || name == "project-id") && !kubernetesSegment(value) {
			return nil, usageError{message: "invalid Docker inventory filter"}
		}
		key := keys[name]
		if key == "" {
			key = name
		}
		query.Set(key, value)
	}
	return query, nil
}

func readDockerInventory[T dockerInventoryRow](ctx context.Context, rt Runtime, client APIClient, format, resource string, query url.Values, page, size int) error {
	transport := *client.httpClient()
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Client = &transport
	var envelope struct {
		Data *dockerInventoryPage[T] `json:"data"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/api/v1/docker/"+resource+"?"+query.Encode(), client.Token, nil, nil, &envelope); err != nil {
		return err
	}
	data := envelope.Data
	if data == nil || data.Total < int64(len(data.Items)) || len(data.Items) > size || data.Page != page || data.PageSize != size {
		return fmt.Errorf("invalid Docker inventory page; no pagination metadata was synthesized")
	}
	seen := map[string]bool{}
	for _, item := range data.Items {
		id := item.inventoryID()
		if !kubernetesSegment(id) || seen[id] {
			return fmt.Errorf("invalid or duplicate Docker inventory ID")
		}
		seen[id] = true
	}
	if data.Items == nil {
		data.Items = []T{}
	}
	data.ConfigurationHidden = true
	return writeWorkbenchOutput(rt, client, format, envelope)
}
