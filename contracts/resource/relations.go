package resource

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const RelationSnapshotFeature = "resource-relations.metadata.v1"
const RelationMaxQueries = 32
const RelationMaxItems = 200
const RelationMaxNodes = 500
const RelationMaxDepth = 8

var ErrRelationSnapshotUnsupported = errors.New("resource relation metadata snapshot unsupported")

func DefaultRelationSources(namespace string) []RelationSource {
	return []RelationSource{
		{APIVersion: "v1", Resource: "pods", Kind: "Pod", Namespace: namespace},
		{APIVersion: "apps/v1", Resource: "deployments", Kind: "Deployment", Namespace: namespace},
		{APIVersion: "apps/v1", Resource: "replicasets", Kind: "ReplicaSet", Namespace: namespace},
		{APIVersion: "apps/v1", Resource: "statefulsets", Kind: "StatefulSet", Namespace: namespace},
		{APIVersion: "apps/v1", Resource: "daemonsets", Kind: "DaemonSet", Namespace: namespace},
		{APIVersion: "batch/v1", Resource: "jobs", Kind: "Job", Namespace: namespace},
		{APIVersion: "batch/v1", Resource: "cronjobs", Kind: "CronJob", Namespace: namespace},
		{APIVersion: "v1", Resource: "services", Kind: "Service", Namespace: namespace},
		{APIVersion: "v1", Resource: "persistentvolumeclaims", Kind: "PersistentVolumeClaim", Namespace: namespace},
		{APIVersion: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespace: namespace},
		{APIVersion: "v1", Resource: "secrets", Kind: "Secret", Namespace: namespace},
		{APIVersion: "networking.k8s.io/v1", Resource: "ingresses", Kind: "Ingress", Namespace: namespace},
	}
}

func IsBuiltinRelationSource(source RelationSource) bool {
	for _, builtin := range DefaultRelationSources(source.Namespace) {
		if builtin == source {
			return true
		}
	}
	return false
}

// RelationSource always names an explicit namespaced API collection. Discovery
// must verify the resource/kind pair before reading it.
type RelationSource struct {
	APIVersion string `json:"apiVersion"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
}

type RelationRule struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Target   RelationSource    `json:"target"`
	Name     string            `json:"name,omitempty"`
	UID      string            `json:"uid,omitempty"`
	Selector map[string]string `json:"selector,omitempty"`
}

type RelationConfig struct {
	Version string           `json:"version"`
	RootUID string           `json:"rootUid"`
	Sources []RelationSource `json:"sources"`
	Rules   []RelationRule   `json:"rules"`
}

type RelationQuery struct {
	ID       string            `json:"id"`
	Source   RelationSource    `json:"source"`
	Name     string            `json:"name,omitempty"`
	Selector map[string]string `json:"selector,omitempty"`
}

type RelationSnapshotRequest struct {
	Queries []RelationQuery `json:"queries"`
}
type RelationOwner struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	UID        string `json:"uid"`
	Controller bool   `json:"controller"`
}

type RelationObject struct {
	Resource ResourceRef     `json:"resource"`
	Owners   []RelationOwner `json:"owners,omitempty"`
}
type RelationCoverage struct {
	QueryID    string `json:"queryId"`
	State      string `json:"state"`
	ReasonCode string `json:"reasonCode,omitempty"`
	Truncated  bool   `json:"truncated"`
}
type RelationSourceResult struct {
	Coverage RelationCoverage `json:"coverage"`
	Objects  []RelationObject `json:"objects"`
}
type RelationSnapshot struct {
	Version string                 `json:"version"`
	Sources []RelationSourceResult `json:"sources"`
}
type RelationNode struct {
	Resource ResourceRef `json:"resource"`
	Role     string      `json:"role"`
	Depth    int         `json:"depth"`
}
type RelationEdge struct {
	FromUID string `json:"fromUid"`
	ToUID   string `json:"toUid"`
	Type    string `json:"type"`
	RuleID  string `json:"ruleId,omitempty"`
}
type ResourceRelationsView struct {
	Root       ResourceRef        `json:"root"`
	Config     RelationConfig     `json:"config"`
	Nodes      []RelationNode     `json:"nodes"`
	Edges      []RelationEdge     `json:"edges"`
	Coverage   []RelationCoverage `json:"coverage"`
	Partial    bool               `json:"partial"`
	ObservedAt string             `json:"observedAt"`
}

var relationDNS = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var relationKind = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
var relationToken = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
var relationLabelKey = regexp.MustCompile(`^[a-zA-Z0-9_.-]+(/[a-zA-Z0-9_.-]+)?$`)

func ValidateRelationSource(s RelationSource) error {
	parts := strings.Split(s.APIVersion, "/")
	if len(parts) > 2 || len(parts) == 0 || len(s.APIVersion) > 320 {
		return fmt.Errorf("invalid resource API version")
	}
	for _, part := range parts {
		if !relationDNS.MatchString(part) {
			return fmt.Errorf("invalid resource API version")
		}
	}
	if !relationDNS.MatchString(s.Resource) || len(s.Resource) > 63 || strings.Contains(s.Resource, ".") || !relationKind.MatchString(s.Kind) || len(s.Kind) > 128 {
		return fmt.Errorf("invalid resource definition")
	}
	if !relationDNS.MatchString(s.Namespace) || strings.Contains(s.Namespace, ".") || len(s.Namespace) > 63 {
		return fmt.Errorf("an explicit namespace is required")
	}
	return nil
}

func validateRelationSelector(selector map[string]string) error {
	if len(selector) > 16 {
		return fmt.Errorf("too many label terms")
	}
	for key, value := range selector {
		if len(key) > 317 || !relationLabelKey.MatchString(key) || len(value) > 63 || (value != "" && !relationToken.MatchString(value)) {
			return fmt.Errorf("invalid label selector")
		}
	}
	return nil
}

func ValidateRelationConfig(config RelationConfig) error {
	if config.Sources == nil || config.Rules == nil {
		return fmt.Errorf("sources and rules must be arrays")
	}
	data, err := json.Marshal(config)
	if err != nil || len(data) > 16<<10 || config.Version != "v1" || strings.TrimSpace(config.RootUID) == "" || len(config.RootUID) > 128 || len(config.Sources) > 8 || len(config.Rules) > 8 {
		return fmt.Errorf("invalid relation configuration or budget")
	}
	for _, source := range config.Sources {
		if err := ValidateRelationSource(source); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, rule := range config.Rules {
		if !relationToken.MatchString(rule.ID) || len(rule.ID) > 64 || seen[rule.ID] {
			return fmt.Errorf("invalid or duplicate rule ID")
		}
		seen[rule.ID] = true
		if err := ValidateRelationSource(rule.Target); err != nil {
			return err
		}
		switch rule.Type {
		case "reference", "manual":
			if !relationDNS.MatchString(rule.Name) || len(rule.Name) > 253 || len(rule.Selector) != 0 || len(rule.UID) > 128 || (rule.Type == "manual" && strings.TrimSpace(rule.UID) == "") {
				return fmt.Errorf("named manual relations require a fixed UID")
			}
		case "selector":
			if rule.Name != "" || rule.UID != "" || len(rule.Selector) == 0 {
				return fmt.Errorf("selector rules must declare labels only")
			}
		default:
			return fmt.Errorf("unknown relation rule type")
		}
		if err := validateRelationSelector(rule.Selector); err != nil {
			return err
		}
	}
	return nil
}

func ValidateRelationSnapshotRequest(input RelationSnapshotRequest) error {
	if len(input.Queries) == 0 || len(input.Queries) > RelationMaxQueries {
		return fmt.Errorf("invalid relation query budget")
	}
	seen := map[string]bool{}
	for _, q := range input.Queries {
		if q.ID == "" || len(q.ID) > 80 || !relationToken.MatchString(q.ID) || seen[q.ID] {
			return fmt.Errorf("invalid query identity")
		}
		seen[q.ID] = true
		if err := ValidateRelationSource(q.Source); err != nil {
			return err
		}
		if q.Name != "" && (!relationDNS.MatchString(q.Name) || len(q.Name) > 253 || len(q.Selector) != 0) {
			return fmt.Errorf("invalid named query")
		}
		if err := validateRelationSelector(q.Selector); err != nil {
			return err
		}
	}
	return nil
}
