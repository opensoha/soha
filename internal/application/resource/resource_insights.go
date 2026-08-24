package resource

import (
	"context"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (g *GenericResources) GetResourceGraph(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (domainresource.ResourceGraph, error) {
	kind, name, namespace = strings.TrimSpace(kind), strings.TrimSpace(name), strings.TrimSpace(namespace)
	if kind == "" || name == "" {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: kind and name are required", apperrors.ErrInvalidArgument)
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	var graph domainresource.ResourceGraph
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, clientErr := g.genericResourceAgentClient(connection)
		if clientErr != nil {
			return domainresource.ResourceGraph{}, clientErr
		}
		graph, err = client.GetResourceGraph(ctx, namespace, kind, name)
	} else if g.resourceGraph == nil {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: direct resource graph is not configured", apperrors.ErrClusterUnready)
	} else {
		graph, err = g.resourceGraph.GetResourceGraph(ctx, clusterID, namespace, kind, name)
	}
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	graph = g.filterAuthorizedGraph(ctx, principal, clusterID, graph)
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed Kubernetes resource relationship graph")
	return graph, nil
}

func (g *GenericResources) GetSecurityPosture(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, limit int) (domainresource.SecurityPosture, error) {
	connection, _, err := g.authorize(ctx, principal, clusterID, "", "Cluster", domainaccess.ActionView)
	if err != nil {
		return domainresource.SecurityPosture{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var posture domainresource.SecurityPosture
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, clientErr := g.genericResourceAgentClient(connection)
		if clientErr != nil {
			return domainresource.SecurityPosture{}, clientErr
		}
		posture, err = client.GetSecurityPosture(ctx, namespace, limit)
	} else if g.security == nil {
		return domainresource.SecurityPosture{}, fmt.Errorf("%w: direct security posture is not configured", apperrors.ErrClusterUnready)
	} else {
		posture, err = g.security.GetSecurityPosture(ctx, clusterID, namespace, limit)
	}
	if err != nil {
		return domainresource.SecurityPosture{}, err
	}
	posture = g.filterAuthorizedFindings(ctx, principal, clusterID, posture)
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, "SecurityPosture", "", string(domainaccess.ActionView), "success", "viewed read-only Kubescape security posture")
	return posture, nil
}

func (g *GenericResources) filterAuthorizedGraph(ctx context.Context, principal domainidentity.Principal, clusterID string, graph domainresource.ResourceGraph) domainresource.ResourceGraph {
	allowed := make(map[string]struct{}, len(graph.Nodes))
	nodes := make([]domainresource.ResourceGraphNode, 0, len(graph.Nodes))
	hidden := 0
	for _, node := range graph.Nodes {
		if _, _, err := g.authorize(ctx, principal, clusterID, node.Resource.Namespace, node.Resource.Kind, domainaccess.ActionView); err != nil {
			hidden++
			continue
		}
		allowed[node.ID] = struct{}{}
		nodes = append(nodes, node)
	}
	edges := make([]domainresource.ResourceGraphEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		_, source := allowed[edge.SourceID]
		_, target := allowed[edge.TargetID]
		if source && target {
			edges = append(edges, edge)
		}
	}
	evidence := make([]domainresource.ResourceEvidence, 0, len(graph.Evidence))
	for _, item := range graph.Evidence {
		if item.ResourceID == "" {
			evidence = append(evidence, item)
			continue
		}
		if _, ok := allowed[item.ResourceID]; ok {
			evidence = append(evidence, item)
		}
	}
	graph.Nodes, graph.Edges, graph.Evidence = nodes, edges, evidence
	if hidden > 0 {
		graph.Warnings = append(graph.Warnings, "Some related resources were hidden by access policy.")
	}
	return graph
}

func (g *GenericResources) filterAuthorizedFindings(ctx context.Context, principal domainidentity.Principal, clusterID string, posture domainresource.SecurityPosture) domainresource.SecurityPosture {
	findings := make([]domainresource.SecurityFinding, 0, len(posture.Findings))
	hidden := 0
	for _, finding := range posture.Findings {
		if finding.Resource == nil {
			findings = append(findings, finding)
			continue
		}
		if _, _, err := g.authorize(ctx, principal, clusterID, finding.Resource.Namespace, finding.Resource.Kind, domainaccess.ActionView); err != nil {
			hidden++
			continue
		}
		findings = append(findings, finding)
	}
	posture.Findings = findings
	if hidden > 0 {
		posture.Counts = domainresource.SecuritySeverityCounts{}
		posture.Warnings = append(posture.Warnings, "Some security findings and aggregate severity totals were hidden by access policy.")
	}
	return posture
}
