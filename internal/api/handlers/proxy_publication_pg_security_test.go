package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	accessdomain "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/infrastructure/proxysource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	pubrepo "github.com/opensoha/soha/internal/repository/proxypublication"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func (f *publicationPG) installProfileHandlers(t *testing.T, keys keyring.Ring) {
	service, err := appnetworkaccess.New(accessrepo.New(f.db), appaccess.NewPermissionResolver(publicationPGRoles{}), publicationPGScope{}, publicationPGAudit{}, publicationPGOperations{}, keys)
	pgRequire(t, err)
	service.SetMihomoBundleValidator(f.bundles)
	h := &NetworkAccessHandler{mihomoProfile: service}
	f.router.POST("/profiles", h.CreateMihomoProfile)
	f.router.PUT("/profiles/:profileID", h.UpdateMihomoProfile)
}
func (f *publicationPG) profileBindingScenario(t *testing.T) {
	f.sql(t, "INSERT INTO network_access_devices(id,owner_user_id,name,platform,status) VALUES('outside-device',?,'Outside scope','macos','active')", f.user)
	input := api.NetworkMihomoProfileInput{Name: "Real bound profile", BypassCidrs: []api.NetworkIPCIDR{}, BypassHosts: []string{}, DeviceID: "px-device", Mode: "managed_follow", Status: "active", DeliveryMode: "mapped_nodes", BundleID: "mapped", SourceRevision: 1, MixedPort: 7890, ControllerPort: 9090, DNSMode: "disabled", SelectorGroup: "Proxy", FailClosed: true}
	var saved accessdomain.MihomoProfile
	publicationResponse(t, f.request(t, "POST", "/profiles", input, false), 201, &saved)
	originalID := saved.ID
	input.Name = "Updated bound profile"
	publicationResponse(t, f.request(t, "PUT", "/profiles/"+saved.ID, input, false), 200, &saved)
	assertUnchanged := func() {
		t.Helper()
		var name string
		pgRequire(t, f.db.Raw("SELECT name FROM network_mihomo_profiles WHERE id=?", originalID).Scan(&name).Error)
		if name != input.Name {
			t.Fatal("invalid profile mutation persisted")
		}
	}
	for _, test := range []struct {
		name   string
		change func(*api.NetworkMihomoProfileInput)
		status int
	}{
		{"mode", func(i *api.NetworkMihomoProfileInput) { i.DeliveryMode = "direct_subscription" }, 409},
		{"source", func(i *api.NetworkMihomoProfileInput) { i.SourceRevision++ }, 409},
		{"org", func(i *api.NetworkMihomoProfileInput) { i.OrgRevision++ }, 409},
		{"device", func(i *api.NetworkMihomoProfileInput) { i.DeviceID = "outside-device" }, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := input
			test.change(&bad)
			publicationResponse(t, f.request(t, "PUT", "/profiles/"+originalID, bad, false), test.status, nil)
			assertUnchanged()
		})
	}
	// Owner is read from the device row, independently of the authenticated operator.
	other := "bdb8bb62-6f91-448e-9356-17f3315023f4"
	f.sql(t, "INSERT INTO users(id,username,email,status) VALUES(?,?,?,'active')", other, other, other+"@example.test")
	f.sql(t, "UPDATE network_access_devices SET owner_user_id=? WHERE id='px-device'", other)
	publicationResponse(t, f.request(t, "PUT", "/profiles/"+originalID, input, false), 403, nil)
	assertUnchanged()
	f.sql(t, "UPDATE network_access_devices SET owner_user_id=? WHERE id='px-device'", f.user)
	raw, _ := json.Marshal(input)
	req := httptest.NewRequest("PUT", "/profiles/"+originalID, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Role", "profiles-only")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("profile-only operator bypassed instance permission")
	}
	assertUnchanged()
	// Temporarily mark this same real record non-active; restore only fixture state.
	f.sql(t, `UPDATE network_proxy_bundles SET record=jsonb_set(record,'{Public,state}','"prepare"') WHERE bundle_id='mapped'`)
	publicationResponse(t, f.request(t, "PUT", "/profiles/"+originalID, input, false), 403, nil)
	assertUnchanged()
	f.sql(t, `UPDATE network_proxy_bundles SET record=jsonb_set(record,'{Public,state}','"activated"') WHERE bundle_id='mapped'`)
	f.sql(t, "DELETE FROM network_mihomo_profiles WHERE id=?", originalID)
}
func (f *publicationPG) sourceSecurityScenario(t *testing.T) {
	var before, after api.NetworkProxyEmployeeProjection
	path := "/profiles/px-profile/projection?processEpoch=boot"
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &before)
	save := func(input api.NetworkProxySourceInput) {
		t.Helper()
		input.ExpectedRevision = f.source.ExpectedRevision + 1
		publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/source", input, false), 200, nil)
		f.source = input
	}
	same := f.source
	same.Name = "Renamed source"
	save(same)
	added := f.source
	added.Nodes = append(append([]api.NetworkProxyNode(nil), added.Nodes...), api.NetworkProxyNode{Protocol: "http", Server: "added-c.example.test", Port: 8082})
	save(added)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &after)
	if before.Credential.CredentialID != after.Credential.CredentialID || before.Credential.Secret != after.Credential.Secret || len(after.Nodes) != 1 || after.Nodes[0].NodeID != f.nodes[1] {
		t.Fatal("rename/add widened or rotated surviving credential")
	}
	restored := f.source
	restored.Nodes = append(restored.Nodes, api.NetworkProxyNode{Protocol: "http", Server: "upstream.example.test", Port: 8080, Username: "private-user", Secret: "private-upstream-secret"})
	save(restored)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &after)
	if len(after.Nodes) != 1 || after.Nodes[0].NodeID != f.nodes[1] {
		t.Fatal("restored source revived old bundle node")
	}
	server, err := f.bundles.ServerPublication(f.ctx, "px-a", "mapped", 1, time.Now().Add(time.Minute))
	pgRequire(t, err)
	if len(server.Nodes) != 1 || server.Nodes[0].NodeID != f.nodes[1] {
		t.Fatal("server topology was not narrowed")
	}
	oldID := after.Credential.CredentialID
	f.sql(t, "UPDATE network_proxy_employee_credentials SET expires_at=now()-interval '1 second',record=jsonb_set(record,'{ExpiresAt}',to_jsonb(now()-interval '1 second')) WHERE id=?", oldID)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &after)
	if after.Credential.CredentialID == oldID || len(after.Nodes) != 1 || after.Nodes[0].NodeID != f.nodes[1] {
		t.Fatal("expired credential revived or ceiling widened")
	}
	publicationResponse(t, f.request(t, "DELETE", "/instances/px-master/sources/source?expectedRevision="+strconv.FormatInt(f.source.ExpectedRevision+1, 10), nil, false), 204, nil)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 403, nil)
	recreated := f.source
	recreated.ExpectedRevision = 0
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/source", recreated, false), 409, nil)
}
func (f *publicationPG) directSecurity(t *testing.T, input api.NetworkProxySourceInput, ir domain.IR) {
	// The existing direct test remains scoped to A. A second explicit bundle has AB.
	prepare := func(id string, revision int64) {
		body := api.NetworkProxyBundleInput{RequestID: id, SourceID: "direct-source", SourceRevision: revision, DeliveryMode: "direct_subscription", Scope: api.NetworkProxyScope{UserIDs: []string{f.user}, DeviceIDs: []string{"px-device"}, NodeIDs: []string{ir.Nodes[0].NodeID, ir.Nodes[1].NodeID}}}
		publicationResponse(t, f.request(t, "PUT", "/instances/px-master/bundles/"+id, body, false), 202, nil)
		publicationResponse(t, f.request(t, "POST", "/instances/px-master/bundles/"+id+"/activate", api.NetworkProxyBundleActivateInput{ExpectedRevision: 1, RequestID: id + "-activate"}, false), 202, nil)
		f.seedProfile(t, id, "direct_subscription")
	}
	prepare("direct-ab", 1)
	path := "/profiles/px-profile/direct?processEpoch=security-boot"
	var out api.NetworkProxyDirectProjection
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &out)
	var directID string
	pgRequire(t, f.db.Raw("SELECT id FROM network_proxy_employee_credentials WHERE bundle_id='direct-ab'").Scan(&directID).Error)
	rename := input
	rename.ExpectedRevision = 1
	rename.Name = "Renamed direct"
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/direct-source", rename, false), 200, nil)
	added := rename
	added.ExpectedRevision = 2
	added.Content = strings.Replace(added.Content, "proxy-groups:", "  - name: C\n    type: http\n    server: direct-c.example.test\n    port: 8082\nproxy-groups:", 1)
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/direct-source", added, false), 200, nil)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &out)
	var renewedID string
	pgRequire(t, f.db.Raw("SELECT id FROM network_proxy_employee_credentials WHERE bundle_id='direct-ab'").Scan(&renewedID).Error)
	if renewedID != directID || strings.Contains(out.Content, "direct-c.example.test") {
		t.Fatal("direct renewal rotated delivery identity or added C")
	}
	// Authentication changes preserve logical node IDs and revoke only that node.
	changed := input
	changed.ExpectedRevision = 3
	changed.Name = "Renamed direct"
	changed.Content = strings.Replace(added.Content, "private-upstream-secret", "changed-auth-secret", 1)
	changedIR, err := proxysource.NewCompiler().Compile(changed)
	pgRequire(t, err)
	if changedIR.Nodes[0].NodeID != ir.Nodes[0].NodeID {
		t.Fatal("auth rotation changed logical node identity")
	}
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/direct-source", changed, false), 200, nil)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &out)
	if strings.Contains(out.Content, "upstream.example.test") && !strings.Contains(out.Content, "excluded-upstream.example.test") {
		t.Fatal("direct survivor missing")
	}
	if strings.Contains(out.Content, "server: upstream.example.test") || strings.Contains(out.Content, "changed-auth-secret") || strings.Contains(out.Content, "private-upstream-secret") || len(out.Groups) != 1 || len(out.Groups[0].NodeIDs) != 1 || out.Groups[0].NodeIDs[0] != ir.Nodes[1].NodeID {
		t.Fatal("auth-changed A survived direct projection")
	}
	restored := input
	restored.ExpectedRevision = 4
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/direct-source", restored, false), 200, nil)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &out)
	if strings.Contains(out.Content, "private-upstream-secret") {
		t.Fatal("old direct bundle regained restored A")
	}
	prepare("direct-new-epoch", 5)
	f.controlRequest(t, "px-endpoint", "GET", path, nil, 200, &out)
	if !strings.Contains(out.Content, "private-upstream-secret") {
		t.Fatal("explicit new bundle could not use restored A")
	}
}

// Committed rows belong only to this random-prefix fixture and are removed in
// reverse FK order. The normal HTTP fixture must have rolled back first.
type publicationRaceSeed struct {
	tables     []string
	rows       map[string][]map[string]any
	source     domain.SourceRecord
	credential domain.Credential
	ids        map[string]string
}

func capturePublicationRace(t *testing.T, f *publicationPG, credentialID string) *publicationRaceSeed {
	t.Helper()
	seed := &publicationRaceSeed{rows: map[string][]map[string]any{}, tables: []string{"users", "network_access_sites", "network_access_devices", "network_access_policy_snapshots", "network_runtime_enrollments", "network_runtime_credentials", "network_runtime_configurations", "network_mihomo_profiles", "network_proxy_instances", "network_proxy_sources", "network_proxy_node_invalidations", "network_proxy_bundles", "network_proxy_employee_credentials", "network_proxy_credential_scopes"}}
	var err error
	seed.source, err = f.store.GetSource(f.ctx, "px-master", "source")
	pgRequire(t, err)
	seed.credential, err = f.store.GetCredential(f.ctx, credentialID)
	pgRequire(t, err)
	for _, table := range seed.tables {
		rows, err := f.db.Raw("SELECT to_jsonb(r) FROM " + table + " r").Rows()
		pgRequire(t, err)
		for rows.Next() {
			var raw []byte
			pgRequire(t, rows.Scan(&raw))
			var item map[string]any
			pgRequire(t, json.Unmarshal(raw, &item))
			seed.rows[table] = append(seed.rows[table], item)
		}
		pgRequire(t, rows.Close())
	}
	prefix := "race-" + uuid.NewString()
	seed.ids = map[string]string{f.user: uuid.NewString()}
	// Every owned textual ID is replaced consistently, including nested JSON.
	for _, table := range seed.tables {
		for _, row := range seed.rows[table] {
			if id, ok := row["id"].(string); ok && id != f.user {
				if table == "users" {
					seed.ids[id] = uuid.NewString()
				} else {
					seed.ids[id] = prefix + "-" + id
				}
			}
		}
	}
	for _, id := range []string{"source", "mapped", "px-profile", "px-site", "px-device", "px-master", "px-a", "px-b", "px-endpoint", "px-other"} {
		seed.ids[id] = prefix + "-" + id
	}
	return seed
}
func raceReplace(v any, ids map[string]string) any {
	switch x := v.(type) {
	case string:
		if replacement, ok := ids[x]; ok {
			return replacement
		}
		return x
	case []any:
		for i := range x {
			x[i] = raceReplace(x[i], ids)
		}
	case map[string]any:
		for k, v := range x {
			x[k] = raceReplace(v, ids)
		}
	}
	return v
}
func (f *publicationPG) concurrentSourceScenario(t *testing.T) {
	seed := f.race
	if seed == nil {
		t.Fatal("missing committed-race input")
	}
	db := f.baseDB
	ctx := context.Background()
	// No fixture transaction remains. Exact instance absence prevents colliding
	// with existing data before any independent committed write.
	var existing int64
	pgRequire(t, db.Raw("SELECT count(*) FROM network_proxy_instances WHERE id='px-master'").Scan(&existing).Error)
	if existing != 0 {
		t.Fatal("HTTP outer transaction did not rollback")
	}
	owned := map[string][]map[string]any{}
	cleanup := func() {
		for i := len(seed.tables) - 1; i >= 0; i-- {
			table := seed.tables[i]
			for _, row := range owned[table] {
				query := db.Table(table)
				switch table {
				case "network_proxy_credential_scopes":
					query = query.Where("identity_key=?", row["identity_key"])
				case "network_access_policy_snapshots":
					query = query.Where("policy_version=?", row["policy_version"])
				case "network_proxy_sources":
					query = query.Where("instance_id=? AND source_id=?", row["instance_id"], row["source_id"])
				case "network_proxy_node_invalidations":
					query = query.Where("instance_id=? AND source_id=? AND node_id=? AND invalidated_source_revision=?", row["instance_id"], row["source_id"], row["node_id"], row["invalidated_source_revision"])
				case "network_proxy_bundles":
					query = query.Where("instance_id=? AND bundle_id=?", row["instance_id"], row["bundle_id"])
				case "network_runtime_configurations":
					query = query.Where("runtime_id=? AND configuration_version=?", row["runtime_id"], row["configuration_version"])
				default:
					query = query.Where("id=?", row["id"])
				}
				if err := query.Delete(map[string]any{}).Error; err != nil {
					t.Errorf("cleanup owned %s failed: %v", table, err)
				}
			}
		}
	}
	t.Cleanup(cleanup)
	for _, table := range seed.tables {
		for _, original := range seed.rows[table] {
			raw, _ := json.Marshal(original)
			var row map[string]any
			pgRequire(t, json.Unmarshal(raw, &row))
			raceReplace(row, seed.ids)
			raw, _ = json.Marshal(row)
			pgRequire(t, db.Exec("INSERT INTO "+table+" SELECT * FROM jsonb_populate_record(NULL::"+table+",?::jsonb)", string(raw)).Error)
			owned[table] = append(owned[table], row)
		}
	}
	remap := func(input, out any) {
		raw, _ := json.Marshal(input)
		var value any
		pgRequire(t, json.Unmarshal(raw, &value))
		raw, _ = json.Marshal(raceReplace(value, seed.ids))
		pgRequire(t, json.Unmarshal(raw, out))
	}
	var source domain.SourceRecord
	remap(seed.source, &source)
	var credential domain.Credential
	remap(seed.credential, &credential)
	// The identity ceiling is keyed by a digest rather than an ID; move it to the
	// remapped credential identity before exercising the production transaction.
	oldKey := domain.CredentialIdentity(seed.credential)
	newKey := domain.CredentialIdentity(credential)
	pgRequire(t, db.Exec("UPDATE network_proxy_credential_scopes SET identity_key=? WHERE identity_key=?", newKey, oldKey).Error)
	for _, row := range owned["network_proxy_credential_scopes"] {
		if row["identity_key"] == oldKey {
			row["identity_key"] = newKey
		}
	}
	source.Public.Revision++
	for node := range source.NodeMaterial {
		source.NodeMaterial[node] = "changed-material"
	}
	tx := db.Begin()
	pgRequire(t, tx.Error)
	defer tx.Rollback()
	pgRequire(t, tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network-access-policy-snapshot:default:default").Error)
	done := make(chan error, 1)
	go func() { _, err := pubrepo.New(db).IssueCredential(ctx, credential); done <- err }()
	waiting := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int64
		pgRequire(t, tx.Exec("SELECT pg_stat_clear_snapshot()").Error)
		pgRequire(t, tx.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory'").Scan(&n).Error)
		if n > 0 {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("credential writer did not wait on source policy lock")
	}
	pgRequire(t, pubrepo.New(tx).SaveSource(ctx, source, source.Public.Revision-1))
	pgRequire(t, tx.Commit().Error)
	select {
	case err := <-done:
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("credential issue after source invalidation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("credential transaction did not finish")
	}
	current, err := pubrepo.New(db).GetCredential(ctx, credential.ID)
	pgRequire(t, err)
	if len(current.NodeIDs) != 0 {
		t.Fatal("concurrent source commit left unauthorized credential scope")
	}
	// SaveSource created exactly one new invalidation row per remaining material.
	for node := range source.NodeMaterial {
		owned["network_proxy_node_invalidations"] = append(owned["network_proxy_node_invalidations"], map[string]any{"instance_id": source.InstanceID, "source_id": source.Public.SourceID, "node_id": node, "invalidated_source_revision": source.Public.Revision})
	}
	t.Logf("committed race fixture %s: advisory wait observed, source committed before denied renewal", source.InstanceID)
}
