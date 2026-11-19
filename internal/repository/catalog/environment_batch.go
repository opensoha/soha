package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) loadEnvironmentTargets(ctx context.Context, environments []domaincatalog.ApplicationEnvironment) error {
	ids := make([]string, 0, len(environments))
	for _, env := range environments {
		ids = append(ids, env.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	targets, err := r.queryReleaseTargets(ctx, "application_environment_id IN (SELECT jsonb_array_elements_text(?::jsonb))", string(encoded))
	if err != nil {
		return err
	}
	byEnvironment := make(map[string][]domaincatalog.ReleaseTarget)
	for _, target := range targets {
		id := target.ApplicationEnvironmentID
		byEnvironment[id] = append(byEnvironment[id], target)
	}
	for i := range environments {
		targets := byEnvironment[environments[i].ID]
		if targets == nil {
			targets = []domaincatalog.ReleaseTarget{}
		}
		environments[i].Targets = targets
	}
	return nil
}

type workflowVersionPin struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
}

type workflowVersionSnapshot struct {
	data   []byte
	digest string
}

func (r *Repository) loadEnvironmentWorkflowVersions(ctx context.Context, environments []domaincatalog.ApplicationEnvironment) error {
	pins := make([]workflowVersionPin, 0)
	seen := make(map[workflowVersionPin]bool)
	for _, env := range environments {
		if env.WorkflowTemplateID == "" {
			continue
		}
		if env.WorkflowTemplateVersion < 1 {
			return fmt.Errorf("%w: published template version is required", apperrors.ErrInvalidArgument)
		}
		pin := workflowVersionPin{ID: strings.TrimSpace(env.WorkflowTemplateID), Version: env.WorkflowTemplateVersion}
		if !seen[pin] {
			seen[pin] = true
			pins = append(pins, pin)
		}
	}
	if len(pins) == 0 {
		return nil
	}
	snapshots, err := r.readEnvironmentWorkflowSnapshots(ctx, pins)
	if err != nil {
		return err
	}
	for i := range environments {
		env := &environments[i]
		if env.WorkflowTemplateID == "" {
			continue
		}
		pin := workflowVersionPin{ID: strings.TrimSpace(env.WorkflowTemplateID), Version: env.WorkflowTemplateVersion}
		snapshot, ok := snapshots[pin]
		if !ok {
			return ErrNotFound
		}
		item, err := decodeTemplateSnapshot[domaincatalog.WorkflowTemplate](snapshot.data, snapshot.digest)
		if err != nil {
			return err
		}
		env.WorkflowTemplate = &item
	}
	return nil
}

func (r *Repository) readEnvironmentWorkflowSnapshots(ctx context.Context, pins []workflowVersionPin) (map[workflowVersionPin]workflowVersionSnapshot, error) {
	encoded, err := json.Marshal(pins)
	if err != nil {
		return nil, err
	}
	rows, err := dbtx.DB(ctx, r.db).Raw(`
		SELECT v.template_id, v.version, v.snapshot, v.content_digest
		FROM jsonb_to_recordset(?::jsonb) AS pin(id text, version bigint)
		JOIN catalog_template_versions v ON v.kind = 'workflow' AND v.template_id = pin.id AND v.version = pin.version
	`, string(encoded)).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	snapshots := make(map[workflowVersionPin]workflowVersionSnapshot, len(pins))
	for rows.Next() {
		var pin workflowVersionPin
		var snapshot workflowVersionSnapshot
		if err := rows.Scan(&pin.ID, &pin.Version, &snapshot.data, &snapshot.digest); err != nil {
			return nil, err
		}
		snapshots[pin] = snapshot
	}
	return snapshots, rows.Err()
}
