package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestPermissionCatalogMetadataMatchesEmbeddedArtifact(t *testing.T) {
	raw := PermissionCatalogJSON()
	var catalog struct {
		CatalogVersion string `json:"catalogVersion"`
		ContentHash    string `json:"contentHash"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("decode permission catalog: %v", err)
	}
	if catalog.CatalogVersion != PermissionCatalogVersion {
		t.Fatalf("catalog version = %q, want %q", catalog.CatalogVersion, PermissionCatalogVersion)
	}
	if catalog.ContentHash != "sha256:"+PermissionCatalogContentSHA256 {
		t.Fatalf("content hash = %q", catalog.ContentHash)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != PermissionCatalogSHA256 {
		t.Fatalf("artifact hash = %q, want %q", got, PermissionCatalogSHA256)
	}
}

func TestPermissionCatalogIncludesVirtualizationStorageView(t *testing.T) {
	var catalog struct {
		Permissions []struct {
			Key        string `json:"key"`
			Action     string `json:"action"`
			RiskLevel  string `json:"riskLevel"`
			Status     string `json:"status"`
			Assignable bool   `json:"assignable"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(PermissionCatalogJSON(), &catalog); err != nil {
		t.Fatalf("decode permission catalog: %v", err)
	}
	for _, permission := range catalog.Permissions {
		if permission.Key == "virtualization.storage.view" {
			if permission.Action != "view" || permission.RiskLevel != "read" || permission.Status != "active" || !permission.Assignable {
				t.Fatalf("unexpected storage permission: %#v", permission)
			}
			return
		}
	}
	t.Fatal("virtualization.storage.view is missing")
}

func TestPermissionCatalogIncludesDeliveryEnvironmentApproval(t *testing.T) {
	var catalog struct {
		Permissions []struct {
			Key            string   `json:"key"`
			Domain         string   `json:"domain"`
			Resource       string   `json:"resource"`
			Action         string   `json:"action"`
			RiskLevel      string   `json:"riskLevel"`
			ScopeKinds     []string `json:"scopeKinds"`
			ApprovalPolicy string   `json:"approvalPolicy"`
			Status         string   `json:"status"`
			Assignable     bool     `json:"assignable"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(PermissionCatalogJSON(), &catalog); err != nil {
		t.Fatalf("decode permission catalog: %v", err)
	}
	for _, permission := range catalog.Permissions {
		if permission.Key != "delivery.application-environments.approve" {
			continue
		}
		if permission.Domain != "delivery" || permission.Resource != "application-environments" || permission.Action != "approve" ||
			permission.RiskLevel != "high" || permission.ApprovalPolicy != "eligible" || permission.Status != "active" ||
			!permission.Assignable || len(permission.ScopeKinds) != 3 || permission.ScopeKinds[0] != "workspace" ||
			permission.ScopeKinds[1] != "project" || permission.ScopeKinds[2] != "resource" {
			t.Fatalf("unexpected delivery environment approval permission: %#v", permission)
		}
		return
	}
	t.Fatal("delivery.application-environments.approve is missing")
}

func TestPermissionCatalogIncludesIndependentResourceCreationEntry(t *testing.T) {
	var catalog struct {
		Permissions []struct {
			Key           string   `json:"key"`
			Status        string   `json:"status"`
			Assignable    bool     `json:"assignable"`
			LegacyAliases []string `json:"legacyAliases"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(PermissionCatalogJSON(), &catalog); err != nil {
		t.Fatalf("decode permission catalog: %v", err)
	}

	found := false
	for _, permission := range catalog.Permissions {
		if permission.Key == "platform.resource.create" {
			t.Fatal("retired platform.resource.create definition is still present")
		}
		for _, alias := range permission.LegacyAliases {
			if alias == "platform.resource.create" {
				t.Fatalf("retired platform.resource.create alias remains on %s", permission.Key)
			}
		}
		if permission.Key == "platform.resource-creation.use" {
			found = true
			if permission.Status != "active" || !permission.Assignable {
				t.Fatalf("resource creation entry must be independently assignable: %#v", permission)
			}
		}
	}
	if !found {
		t.Fatal("platform.resource-creation.use is missing")
	}
}

func TestPermissionCatalogIncludesIndependentWorkbenchEntries(t *testing.T) {
	var catalog struct {
		Permissions []struct {
			Key        string `json:"key"`
			Domain     string `json:"domain"`
			Action     string `json:"action"`
			Status     string `json:"status"`
			Assignable bool   `json:"assignable"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(PermissionCatalogJSON(), &catalog); err != nil {
		t.Fatalf("decode permission catalog: %v", err)
	}

	want := map[string]bool{
		"workbench.ai.view":         false,
		"workbench.compute.view":    false,
		"workbench.delivery.view":   false,
		"workbench.home.view":       false,
		"workbench.monitoring.view": false,
		"workbench.platform.view":   false,
		"workbench.security.view":   false,
		"workbench.settings.view":   false,
	}
	for _, permission := range catalog.Permissions {
		if _, ok := want[permission.Key]; !ok {
			continue
		}
		if permission.Domain != "workbench" || permission.Action != "view" || permission.Status != "active" || !permission.Assignable {
			t.Fatalf("workbench entry must be independently assignable: %#v", permission)
		}
		want[permission.Key] = true
	}
	for key, found := range want {
		if !found {
			t.Fatalf("%s is missing", key)
		}
	}
}
