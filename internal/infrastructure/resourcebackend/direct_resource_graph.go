package resourcebackend

import (
	"context"
	"fmt"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) GetResourceGraph(ctx context.Context, clusterID, namespace, kind, name string) (domainresource.ResourceGraph, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	snapshot := resourceGraphSnapshot{}
	if items, listErr := bundle.Typed.AppsV1().Deployments(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.deployments = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "Deployments were unavailable while building the graph.")
	}
	if items, listErr := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.replicaSets = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "ReplicaSets were unavailable while building the graph.")
	}
	if items, listErr := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.pods = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "Pods were unavailable while building the graph.")
	}
	if items, listErr := bundle.Typed.CoreV1().Services(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.services = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "Services were unavailable while building the graph.")
	}
	if items, listErr := bundle.Typed.NetworkingV1().Ingresses(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.ingresses = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "Ingresses were unavailable while building the graph.")
	}
	if items, listErr := bundle.Typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.hpas = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "HorizontalPodAutoscalers were unavailable while building the graph.")
	}
	if items, listErr := bundle.Typed.PolicyV1().PodDisruptionBudgets(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.pdbs = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "PodDisruptionBudgets were unavailable while building the graph.")
	}
	if items, listErr := bundle.Typed.CoreV1().Events(namespace).List(queryCtx, metav1.ListOptions{}); listErr == nil {
		snapshot.events = items.Items
	} else {
		snapshot.warnings = append(snapshot.warnings, "Kubernetes events were unavailable while building the graph.")
	}
	graph, err := buildResourceGraph(clusterID, namespace, kind, name, snapshot)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	if len(graph.Nodes) > 500 {
		return domainresource.ResourceGraph{}, fmt.Errorf("resource graph exceeds the 500 node safety limit")
	}
	return graph, nil
}
