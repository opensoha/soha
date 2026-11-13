package kubernetes

import (
	"context"
	"errors"
	"fmt"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func isGitOpsManifestTask(payload sohaapi.ManifestExecutionTaskPayload) bool {
	if len(payload.GitOpsDocuments) > 0 {
		return true
	}
	for _, document := range payload.Documents {
		if document.APIVersion == "argoproj.io/v1alpha1" && document.Kind == "Application" {
			return true
		}
	}
	return false
}

func (c *Client) executeGitOpsManifest(ctx context.Context, mapper manifestRESTMapper, payload sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	result := sohaapi.ManifestExecutionTaskResult{Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation, RenderedDigest: payload.RenderedDigest, Inventory: []sohaapi.ManifestResourceInventory{}, Diagnostics: []sohaapi.ManifestDiagnostic{}}
	if len(payload.Documents) != 1 || payload.BindingID == "" || payload.FieldManager != "opensoha-manifest/"+payload.BindingID || payload.ForceConflicts || string(payload.Action) == "adopt" {
		return result, fmt.Errorf("GitOps requires one frozen Application and its stable binding owner; force and adoption are disabled")
	}
	document := payload.Documents[0]
	desired, mapping, err := prepareManifestDocument(mapper, payload, document)
	if err != nil {
		return result, err
	}
	children := make([]*unstructured.Unstructured, 0, len(payload.GitOpsDocuments))
	for _, child := range payload.GitOpsDocuments {
		object, _, err := prepareManifestDocument(mapper, payload, child)
		if err != nil {
			return result, err
		}
		children = append(children, object)
	}
	if err := resourceruntime.ValidateArgoResources(desired, children); err != nil {
		return result, err
	}
	options := metav1.PatchOptions{FieldManager: payload.FieldManager}
	if string(payload.Action) == "preflight" {
		options.DryRun = []string{metav1.DryRunAll}
		_, err := resourceruntime.ApplyArgoApplication(ctx, c.dynamic, desired, children, options, payload.IdempotencyKey)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, manifestDiagnostic("dry_run", document, err))
		}
		result.Preflight = &sohaapi.ManifestPreflightResult{Ready: err == nil, Capability: "available", RenderedDigest: payload.RenderedDigest, ResourceCount: 1 + len(children), Diagnostics: result.Diagnostics}
		return result, nil
	}
	var live *unstructured.Unstructured
	if string(payload.Action) == "observe" {
		queryCtx, cancel := context.WithTimeout(ctx, manifestTaskTimeout)
		live, err = manifestResource(c.dynamic, mapping, document.Namespace).Get(queryCtx, document.Name, metav1.GetOptions{})
		cancel()
	} else {
		live, err = resourceruntime.ApplyArgoApplication(ctx, c.dynamic, desired, children, options, payload.IdempotencyKey)
	}
	if err != nil {
		stage := "apply"
		if string(payload.Action) == "observe" {
			stage = "observe"
		}
		result.Diagnostics = append(result.Diagnostics, manifestDiagnostic(stage, document, err))
	}
	if live == nil || ctx.Err() != nil {
		return result, err
	}
	observed, observeErr := resourceruntime.ObserveArgoApplication(ctx, c.dynamic, desired, live, children, payload.FieldManager)
	if observeErr != nil {
		result.Diagnostics = append(result.Diagnostics, manifestDiagnostic("observe", document, observeErr))
	}
	item := manifestInventory(payload, document, desired, live)
	item.Health = observed.Health
	result.Inventory = append(result.Inventory, item)
	driftResources := []manifestDriftResource{}
	appendDrift := func(document sohaapi.ManifestRenderedDocument, wanted, actual *unstructured.Unstructured) {
		if fields := diffManifestFields(wanted.Object, actual.Object, ""); len(fields) > 0 {
			for i := range fields {
				fields[i].FieldManager = ""
			}
			driftResources = append(driftResources, manifestDriftResource{APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name, Fields: fields})
		}
	}
	appendDrift(document, desired, live)
	for i, child := range children {
		for _, actual := range observed.Resources {
			if child.GroupVersionKind() == actual.GroupVersionKind() && child.GetName() == actual.GetName() && child.GetNamespace() == actual.GetNamespace() {
				result.Inventory = append(result.Inventory, manifestInventory(payload, payload.GitOpsDocuments[i], child, actual))
				appendDrift(payload.GitOpsDocuments[i], child, actual)
				break
			}
		}
	}
	result.Drift = buildManifestDriftReport(driftResources)
	result.Drift.Drifted = result.Drift.Drifted || observed.Drifted
	if observed.OperationID != "" {
		result.EvidenceRefs = []string{"argocd:" + live.GetNamespace() + "/" + live.GetName() + ":" + observed.OperationID}
	}
	return result, errors.Join(err, observeErr)
}
