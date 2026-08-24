package resourcebackend

import (
	"context"
	"fmt"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	kubescapeConfigurationSummaryGVR = schema.GroupVersionResource{Group: "spdx.softwarecomposition.kubescape.io", Version: "v1beta1", Resource: "workloadconfigurationscansummaries"}
	kubescapeVulnerabilitySummaryGVR = schema.GroupVersionResource{Group: "spdx.softwarecomposition.kubescape.io", Version: "v1beta1", Resource: "vulnerabilitymanifestsummaries"}
)

func (d *Direct) GetSecurityPosture(ctx context.Context, clusterID, namespace string, limit int) (domainresource.SecurityPosture, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.SecurityPosture{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	configurations, configErr := listOptionalKubescapeResources(queryCtx, bundle.Dynamic, kubescapeConfigurationSummaryGVR, namespace)
	vulnerabilities, vulnerabilityErr := listOptionalKubescapeResources(queryCtx, bundle.Dynamic, kubescapeVulnerabilitySummaryGVR, namespace)
	if apierrors.IsNotFound(configErr) && apierrors.IsNotFound(vulnerabilityErr) {
		return buildKubescapePosture(clusterID, namespace, nil, nil, limit), nil
	}
	posture := buildKubescapePosture(clusterID, namespace, configurations, vulnerabilities, limit)
	for kind, listErr := range map[string]error{"configuration summaries": configErr, "vulnerability summaries": vulnerabilityErr} {
		if listErr == nil || apierrors.IsNotFound(listErr) {
			continue
		}
		posture.Status = "partial"
		posture.Warnings = append(posture.Warnings, fmt.Sprintf("Kubescape %s were unavailable.", kind))
	}
	if len(configurations) == 0 && len(vulnerabilities) == 0 && (configErr != nil || vulnerabilityErr != nil) {
		posture.Status = "degraded"
		posture.Message = "Kubescape results could not be read with the current cluster credentials."
	}
	return posture, nil
}

func listOptionalKubescapeResources(ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	resource := client.Resource(gvr)
	var interfaceClient dynamic.ResourceInterface = resource
	if namespace != "" {
		interfaceClient = resource.Namespace(namespace)
	}
	items := []unstructured.Unstructured{}
	options := metav1.ListOptions{Limit: 500}
	for len(items) < 2000 {
		page, err := interfaceClient.List(ctx, options)
		if err != nil {
			return items, err
		}
		items = append(items, page.Items...)
		if page.GetContinue() == "" {
			return items, nil
		}
		options.Continue = page.GetContinue()
	}
	return items, nil
}
