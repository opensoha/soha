package manifestruntime

import (
	"context"
	"errors"
	"fmt"
	"time"

	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

func isGitOpsTask(payload domainmanifest.TaskPayload) bool {
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

func executeGitOpsDocuments(ctx context.Context, client dynamic.Interface, mapper restMapper, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	result := domainmanifest.TaskResult{Action: payload.Action, DeploymentID: payload.DeploymentID, Generation: payload.Generation, RenderedDigest: payload.RenderedDigest, Inventory: []domainmanifest.ResourceInventory{}, Diagnostics: []domainmanifest.Diagnostic{}}
	if len(payload.Documents) != 1 || payload.BindingID == "" || payload.FieldManager != "opensoha-manifest/"+payload.BindingID || payload.ForceConflicts || payload.Action == domainmanifest.TaskActionAdopt {
		return result, fmt.Errorf("GitOps requires one frozen Application and its stable binding owner; force and adoption are disabled")
	}
	document := payload.Documents[0]
	desired, mapping, err := prepareRuntimeDocument(mapper, payload, document)
	if err != nil {
		return result, err
	}
	children := make([]*unstructured.Unstructured, 0, len(payload.GitOpsDocuments))
	for _, child := range payload.GitOpsDocuments {
		object, _, err := prepareRuntimeDocument(mapper, payload, child)
		if err != nil {
			return result, err
		}
		children = append(children, object)
	}
	if err := resourceruntime.ValidateArgoResources(desired, children); err != nil {
		return result, err
	}
	options := metav1.PatchOptions{FieldManager: payload.FieldManager}
	if payload.Action == domainmanifest.TaskActionPreflight {
		options.DryRun = []string{metav1.DryRunAll}
		_, err := resourceruntime.ApplyArgoApplication(ctx, client, desired, children, options, payload.IdempotencyKey)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, runtimeDiagnostic("dry_run", document, err))
		}
		result.Preflight = &domainmanifest.PreflightResult{Ready: err == nil, Capability: "available", RenderedDigest: payload.RenderedDigest, ResourceCount: 1 + len(children), Diagnostics: result.Diagnostics}
		return result, nil
	}
	var live *unstructured.Unstructured
	if payload.Action == domainmanifest.TaskActionObserve {
		queryCtx, cancel := context.WithTimeout(ctx, manifestRuntimeTimeout)
		live, err = resourceInterface(client, mapping, document.Namespace).Get(queryCtx, document.Name, metav1.GetOptions{})
		cancel()
	} else {
		live, err = resourceruntime.ApplyArgoApplication(ctx, client, desired, children, options, payload.IdempotencyKey)
	}
	if err != nil {
		stage := "apply"
		if payload.Action == domainmanifest.TaskActionObserve {
			stage = "observe"
		}
		result.Diagnostics = append(result.Diagnostics, runtimeDiagnostic(stage, document, err))
	}
	if live == nil || ctx.Err() != nil {
		return result, err
	}
	observed, observeErr := resourceruntime.ObserveArgoApplication(ctx, client, desired, live, children, payload.FieldManager)
	if observeErr != nil {
		result.Diagnostics = append(result.Diagnostics, runtimeDiagnostic("observe", document, observeErr))
	}
	appendGitOpsObservation(&result, payload, desired, live, children, observed)
	return result, errors.Join(err, observeErr)
}

func appendGitOpsObservation(result *domainmanifest.TaskResult, payload domainmanifest.TaskPayload, desired, live *unstructured.Unstructured, children []*unstructured.Unstructured, observed resourceruntime.ArgoObservation) {
	document := payload.Documents[0]
	item := inventoryItem(payload, document, desired, live)
	item.Health = observed.Health
	result.Inventory = append(result.Inventory, item)
	drift := &domainmanifest.DriftReport{Drifted: observed.Drifted, ObservedAt: time.Now().UTC(), Resources: []domainmanifest.DriftResource{}, EvidenceRefs: []string{}}
	appendDrift := func(document domainmanifest.RenderedDocument, wanted, actual *unstructured.Unstructured) {
		if fields := diffDesiredFields(wanted.Object, actual.Object, ""); len(fields) > 0 {
			drift.Drifted = true
			drift.Resources = append(drift.Resources, domainmanifest.DriftResource{APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name, Fields: fields})
		}
	}
	appendDrift(document, desired, live)
	for i, child := range children {
		for _, actual := range observed.Resources {
			if child.GroupVersionKind() == actual.GroupVersionKind() && child.GetName() == actual.GetName() && child.GetNamespace() == actual.GetNamespace() {
				result.Inventory = append(result.Inventory, inventoryItem(payload, payload.GitOpsDocuments[i], child, actual))
				appendDrift(payload.GitOpsDocuments[i], child, actual)
				break
			}
		}
	}
	result.Drift = drift
	if observed.OperationID != "" {
		result.EvidenceRefs = []string{"argocd:" + live.GetNamespace() + "/" + live.GetName() + ":" + observed.OperationID}
	}
}
