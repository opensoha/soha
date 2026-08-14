package traces

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/telemetry"
)

const skyWalkingListServicesQuery = `
query ListServices($layer: String) {
  listServices(layer: $layer) { id name shortName }
}
`

const skyWalkingGetServiceQuery = `
query GetService($serviceKey: String!, $serviceId: ID!, $duration: Duration!) {
  getService(serviceId: $serviceKey) { id name shortName }
  listInstances(duration: $duration, serviceId: $serviceId) { id name }
  findEndpoint(serviceId: $serviceId, limit: 200, duration: $duration) { id name }
}
`

const skyWalkingServiceTopologyQuery = `
query GetServiceTopology($serviceId: ID!, $duration: Duration!) {
  getServiceTopology(serviceId: $serviceId, duration: $duration, debug: false) {
    nodes { id name }
    calls { source target }
  }
}
`

type skyWalkingServiceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
}

type skyWalkingServiceListPayload struct {
	Data struct {
		ListServices []skyWalkingServiceInfo `json:"listServices"`
	} `json:"data"`
	Errors []skyWalkingError `json:"errors"`
}

type skyWalkingServiceDetailPayload struct {
	Data struct {
		GetService    *skyWalkingServiceInfo `json:"getService"`
		ListInstances []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"listInstances"`
		FindEndpoint []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"findEndpoint"`
	} `json:"data"`
	Errors []skyWalkingError `json:"errors"`
}

type skyWalkingTopologyPayload struct {
	Data struct {
		Topology *struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"nodes"`
			Calls []struct {
				Source string `json:"source"`
				Target string `json:"target"`
			} `json:"calls"`
		} `json:"getServiceTopology"`
	} `json:"data"`
	Errors []skyWalkingError `json:"errors"`
}

func (d skyWalkingDriver) ListServices(ctx context.Context, sourceID string, config map[string]any, query telemetry.ServiceQuery) (telemetry.ServiceResult, error) {
	variables := map[string]any{}
	if layer := strings.TrimSpace(stringValue(config["layer"], "")); layer != "" {
		variables["layer"] = layer
	}
	var payload skyWalkingServiceListPayload
	if err := d.queryServiceGraphQL(ctx, config, skyWalkingListServicesQuery, variables, &payload); err != nil {
		return telemetry.ServiceResult{}, err
	}
	if err := skyWalkingPayloadError(payload.Errors); err != nil {
		return telemetry.ServiceResult{}, err
	}
	filter := strings.ToLower(strings.TrimSpace(query.ServiceName))
	services := make([]telemetry.Service, 0, len(payload.Data.ListServices))
	for _, item := range payload.Data.ListServices {
		if filter != "" && !strings.Contains(strings.ToLower(item.Name), filter) && !strings.Contains(strings.ToLower(item.ShortName), filter) {
			continue
		}
		services = append(services, publicSkyWalkingService(item))
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return telemetry.ServiceResult{SourceID: sourceID, Services: services}, nil
}

func (d skyWalkingDriver) GetService(ctx context.Context, _ string, config map[string]any, query telemetry.ServiceQuery) (telemetry.Service, error) {
	if strings.TrimSpace(query.ServiceID) == "" {
		return telemetry.Service{}, fmt.Errorf("skywalking service ID is required")
	}
	var payload skyWalkingServiceDetailPayload
	if err := d.queryServiceGraphQL(ctx, config, skyWalkingGetServiceQuery, map[string]any{
		"serviceKey": query.ServiceID,
		"serviceId":  query.ServiceID,
		"duration":   skyWalkingDuration(query.TimeFrom, query.TimeTo),
	}, &payload); err != nil {
		return telemetry.Service{}, err
	}
	if err := skyWalkingPayloadError(payload.Errors); err != nil {
		return telemetry.Service{}, err
	}
	if payload.Data.GetService == nil {
		return telemetry.Service{}, nil
	}
	service := publicSkyWalkingService(*payload.Data.GetService)
	for _, item := range payload.Data.ListInstances {
		service.Instances = append(service.Instances, telemetry.ServiceInstance{ID: item.ID, Name: item.Name})
	}
	for _, item := range payload.Data.FindEndpoint {
		service.Endpoints = append(service.Endpoints, telemetry.ServiceEndpoint{ID: item.ID, Name: item.Name})
	}
	return service, nil
}

func (d skyWalkingDriver) GetServiceTopology(ctx context.Context, sourceID string, config map[string]any, query telemetry.ServiceQuery) (telemetry.ServiceTopology, error) {
	if strings.TrimSpace(query.ServiceID) == "" {
		return telemetry.ServiceTopology{}, fmt.Errorf("skywalking service ID is required")
	}
	var payload skyWalkingTopologyPayload
	if err := d.queryServiceGraphQL(ctx, config, skyWalkingServiceTopologyQuery, map[string]any{
		"serviceId": query.ServiceID,
		"duration":  skyWalkingDuration(query.TimeFrom, query.TimeTo),
	}, &payload); err != nil {
		return telemetry.ServiceTopology{}, err
	}
	if err := skyWalkingPayloadError(payload.Errors); err != nil {
		return telemetry.ServiceTopology{}, err
	}
	result := telemetry.ServiceTopology{SourceID: sourceID, Nodes: []telemetry.ServiceTopologyNode{}, Edges: []telemetry.ServiceTopologyEdge{}}
	if payload.Data.Topology == nil {
		return result, nil
	}
	for _, item := range payload.Data.Topology.Nodes {
		result.Nodes = append(result.Nodes, telemetry.ServiceTopologyNode{ServiceID: item.ID, Name: item.Name})
	}
	for _, item := range payload.Data.Topology.Calls {
		result.Edges = append(result.Edges, telemetry.ServiceTopologyEdge{SourceServiceID: item.Source, TargetServiceID: item.Target})
	}
	return result, nil
}

func (d skyWalkingDriver) queryServiceGraphQL(ctx context.Context, config map[string]any, query string, variables map[string]any, target any) error {
	if err := d.ValidateConfig(config); err != nil {
		return err
	}
	req, err := newSkyWalkingGraphQLRequest(ctx, config, map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	return d.doSkyWalking(req, target)
}

func skyWalkingDuration(from, to time.Time) map[string]any {
	return map[string]any{
		"start": from.UTC().Format("2006-01-02 1504"),
		"end":   to.UTC().Format("2006-01-02 1504"),
		"step":  "MINUTE",
	}
}

func skyWalkingPayloadError(errors []skyWalkingError) error {
	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("skywalking query failed: %s", errors[0].Message)
}

func publicSkyWalkingService(item skyWalkingServiceInfo) telemetry.Service {
	displayName := strings.TrimSpace(item.ShortName)
	if displayName == "" {
		displayName = strings.TrimSpace(item.Name)
	}
	return telemetry.Service{
		ID: strings.TrimSpace(item.ID), Name: strings.TrimSpace(item.Name), DisplayName: displayName,
		Instances: []telemetry.ServiceInstance{}, Endpoints: []telemetry.ServiceEndpoint{},
	}
}
