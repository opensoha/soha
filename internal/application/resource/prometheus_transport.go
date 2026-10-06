package resource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type prometheusClusterKey struct{}

func (s *metricsSupport) doPrometheusRequest(ctx context.Context, request *http.Request) (*http.Response, error) {
	if clusterID, _ := ctx.Value(prometheusClusterKey{}).(string); clusterID != "" && s.resolver != nil {
		connection, err := s.resolver.GetConnection(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		if metadataValue(connection.Metadata, "prometheus_transport") == "agent" {
			if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent || s.agent == nil {
				return nil, fmt.Errorf("%w: Prometheus Agent transport is unavailable", apperrors.ErrUnsupportedOperation)
			}
			client, err := s.agent(connection)
			if err != nil {
				return nil, err
			}
			params := request.URL.Query()
			query := contractresource.PrometheusQuery{Endpoint: strings.TrimRight(metadataValue(connection.Metadata, "prometheus_url"), "/"), Query: params.Get("query"), Kind: "instant"}
			if strings.HasSuffix(request.URL.Path, "/query_range") {
				query.Kind = "range"
				query.Start, _ = strconv.ParseInt(params.Get("start"), 10, 64)
				query.End, _ = strconv.ParseInt(params.Get("end"), 10, 64)
				query.Step, _ = strconv.ParseInt(params.Get("step"), 10, 64)
			}
			data, err := client.QueryPrometheus(ctx, query)
			if err != nil {
				return nil, wrapAgentResourceError(err)
			}
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(data))}, nil
		}
	}
	// Never forward bearer credentials through redirects, including Core direct mode.
	client := *s.httpClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: Prometheus endpoint is unreachable", apperrors.ErrClusterUnready)
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: Prometheus redirects are not allowed", apperrors.ErrClusterUnready)
	}
	return response, nil
}
