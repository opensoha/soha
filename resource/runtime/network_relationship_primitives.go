package runtime

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"
)

// servicePodSelector preserves Service's nonempty equality-selector semantics.
// Malformed values invalidate the selector rather than silently broadening it.
func servicePodSelector(selector map[string]any) labels.Selector {
	if len(selector) == 0 {
		return nil
	}
	set := labels.Set{}
	for key, value := range selector {
		label, ok := value.(string)
		if !ok {
			return nil
		}
		set[key] = label
	}
	return labels.SelectorFromSet(set)
}

type ingressBackendDeclaration struct {
	host            string
	path            map[string]any
	serviceName     string
	port            string
	fieldPath       string
	serviceDeclared bool
}

// forEachIngressBackendDeclaration extracts configured references once. Callers
// retain their own graph projection, resolution, authorization and budget rules.
func forEachIngressBackendDeclaration(body map[string]any, active func() bool, visit func(ingressBackendDeclaration)) {
	if !active() {
		return
	}
	if backend := object(at(body, "spec", "defaultBackend")); len(backend) > 0 {
		visit(ingressBackendFact("", map[string]any{"path": "/", "pathType": "Default"}, backend, "spec.defaultBackend"))
	}
	rules, _ := at(body, "spec", "rules").([]any)
	for ruleIndex, raw := range rules {
		if !active() {
			return
		}
		rule := object(raw)
		paths, _ := at(rule, "http", "paths").([]any)
		for pathIndex, rawPath := range paths {
			if !active() {
				return
			}
			path := object(rawPath)
			visit(ingressBackendFact(text(rule["host"]), path, object(path["backend"]), fmt.Sprintf("spec.rules[%d].http.paths[%d].backend", ruleIndex, pathIndex)))
		}
	}
}

func ingressBackendFact(host string, path, backend map[string]any, fieldPath string) ingressBackendDeclaration {
	service := object(backend["service"])
	name, _ := service["name"].(string)
	port := object(service["port"])
	return ingressBackendDeclaration{host: host, path: path, serviceName: name, port: firstTopology(text(port["name"]), text(port["number"])), fieldPath: fieldPath, serviceDeclared: len(service) > 0}
}
