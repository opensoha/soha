package sohacli

import (
	"net/http"
	"net/url"
	"strconv"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func deliveryManifestRequest(resource, action string, ids []string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	id, err := deliveryWorkbenchID(ids, action != "list" && action != "create")
	if err != nil {
		return deliveryWorkbenchRequest{}, err
	}
	switch resource {
	case "manifests":
		return deliveryManifestPackageRequest(action, id, opt)
	case "manifest-bindings":
		return deliveryManifestBindingRequest(action, id, opt)
	case "manifest-deployments":
		return deliveryManifestDeploymentRequest(action, id, opt)
	}
	return deliveryWorkbenchRequest{}, deliveryUnsupportedAction(resource, action)
}

func deliveryManifestPackageRequest(action, id string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	req := deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/delivery/manifest-packages", id: id}
	if id != "" {
		req.path += "/" + url.PathEscape(id)
	}
	switch action {
	case "list":
		query, err := deliveryManifestFilters(false, opt)
		if err != nil {
			return req, err
		}
		req.path, req.out = deliveryQuery(req.path, query), &sohaapi.ManifestPackagePageEnvelope{}
		req.allowed = []string{"application-id", "service-id", "cluster-id", "namespace", "search", "page", "page-size"}
	case "get":
		req.out = &sohaapi.ManifestPackageEnvelope{}
	case "revisions":
		req.path, req.out, req.list = req.path+"/revisions", &sohaapi.ManifestRevisionListEnvelope{}, true
	case "bindings":
		req.path, req.out, req.list = req.path+"/bindings", &sohaapi.ManifestBindingListEnvelope{}, true
	default:
		return req, deliveryUnsupportedAction("manifests", action)
	}
	return req, nil
}

func deliveryManifestBindingRequest(action, id string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	req := deliveryWorkbenchRequest{method: http.MethodPut, path: "/api/v1/delivery/manifest-bindings/" + url.PathEscape(id), id: id, out: &sohaapi.ManifestBindingEnvelope{}}
	switch action {
	case "create":
		if _, err := deliveryWorkbenchID([]string{opt.packageID}, true); err != nil {
			return req, usageError{message: "manifest binding creation requires --package-id; after an uncertain response inspect that package's bindings"}
		}
		req.method, req.path = http.MethodPost, "/api/v1/delivery/manifest-packages/"+url.PathEscape(opt.packageID)+"/bindings"
		req.id, req.body, req.allowed = opt.packageID, &sohaapi.ManifestBindingInput{}, []string{"package-id"}
	case "update":
		req.body = &sohaapi.ManifestBindingUpdateInput{}
	case "delete":
		req.method, req.out = http.MethodDelete, &sohaapi.OperationStatus{}
	case "set-revision":
		req.method, req.path = http.MethodPost, req.path+"/desired-revision"
		req.body, req.out = &sohaapi.ManifestDesiredRevisionInput{}, &sohaapi.ManifestDeploymentActionEnvelope{}
	default:
		return req, deliveryUnsupportedAction("manifest-bindings", action)
	}
	return req, nil
}

func deliveryManifestDeploymentRequest(action, id string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	req := deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/delivery/manifest-deployments", id: id, out: &sohaapi.ManifestDeploymentEnvelope{}}
	if id != "" {
		req.path += "/" + url.PathEscape(id)
	}
	switch action {
	case "list":
		query, err := deliveryManifestFilters(true, opt)
		if err != nil {
			return req, err
		}
		req.path, req.out = deliveryQuery(req.path, query), &sohaapi.ManifestDeploymentListEnvelope{}
		req.allowed = []string{"package-id", "application-id", "application-environment-id", "cluster-id", "namespace", "source-mode", "phase", "page", "page-size"}
	case "get":
	case "reconcile", "repair", "adopt", "rollback":
		req.method, req.path = http.MethodPost, req.path+"/"+action
		req.body, req.out = &sohaapi.ManifestDeploymentActionInput{}, &sohaapi.ManifestDeploymentActionEnvelope{}
		if action == "rollback" {
			req.body = &sohaapi.ManifestRollbackInput{}
		}
	default:
		return req, deliveryUnsupportedAction("manifest-deployments", action)
	}
	return req, nil
}

func deliveryManifestFilters(deployments bool, opt deliveryWorkbenchOptions) (url.Values, error) {
	maximum := 200
	filters := map[string]string{"applicationId": opt.application, "clusterId": opt.cluster, "namespace": opt.namespace}
	if deployments {
		maximum = 100
		filters["packageId"], filters["applicationEnvironmentId"], filters["phase"], filters["sourceMode"] = opt.packageID, opt.binding, opt.phase, opt.sourceMode
		if opt.phase != "" && !sohaapi.ManifestDeploymentPhase(opt.phase).Valid() || opt.sourceMode != "" && !sohaapi.ManifestSourceMode(opt.sourceMode).Valid() {
			return nil, usageError{message: "unsupported manifest phase or source mode"}
		}
	} else {
		filters["serviceId"] = opt.service
	}
	if opt.page < 1 || opt.pageSize < 1 || opt.pageSize > maximum || len(opt.search) > 1024 {
		return nil, usageError{message: "invalid manifest page or page size; search is bounded to 1024 bytes"}
	}
	query := url.Values{"page": {strconv.Itoa(opt.page)}, "pageSize": {strconv.Itoa(opt.pageSize)}}
	for key, value := range filters {
		if err := deliveryFilter(query, key, value); err != nil {
			return nil, err
		}
	}
	if !deployments && opt.search != "" {
		query.Set("search", opt.search)
	}
	return query, nil
}

func validateDeliveryManifestInput(input any) error {
	switch value := input.(type) {
	case *sohaapi.ManifestBindingInput:
		if deliveryManifestBindingFieldsValid(value.ApplicationEnvironmentID, value.ClusterID, value.Namespace, value.DeletionPolicy, value.DriftPolicy) {
			return nil
		}
	case *sohaapi.ManifestBindingUpdateInput:
		if value.ExpectedVersion > 0 && deliveryManifestBindingFieldsValid(value.ApplicationEnvironmentID, value.ClusterID, value.Namespace, value.DeletionPolicy, value.DriftPolicy) {
			return nil
		}
	case *sohaapi.ManifestDesiredRevisionInput:
		if value.ExpectedGeneration > 0 && value.DesiredRevision > 0 && value.ReconcilePolicy.Valid() {
			return nil
		}
	case *sohaapi.ManifestDeploymentActionInput:
		if value.ExpectedGeneration > 0 {
			return nil
		}
	case *sohaapi.ManifestRollbackInput:
		if value.ExpectedGeneration > 0 && (value.UseLastKnownGood && value.TargetRevision == 0 || !value.UseLastKnownGood && value.TargetRevision > 0) {
			return nil
		}
	}
	return usageError{message: "invalid manifest input: supply the required scope, supported policy and reviewed version/generation; rollback needs exactly one revision selector"}
}

func deliveryManifestBindingFieldsValid(environment, cluster, namespace string, deletion sohaapi.ManifestDeletionPolicy, drift sohaapi.ManifestDriftPolicy) bool {
	return environment != "" && cluster != "" && namespace != "" && deletion.Valid() && drift.Valid()
}
