package docker

import (
	"context"
	"encoding/json"
	"time"

	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) PatchHostConfiguration(ctx context.Context, id string, input domaindocker.HostInput, fields []string) (domaindocker.Host, error) {
	var result domaindocker.Host
	err := dbtx.Within(ctx, r.db, func(txctx context.Context) error {
		var err error
		result, err = r.patchHostConfiguration(txctx, id, input, fields)
		return err
	})
	return result, err
}

func (r *Repository) patchHostConfiguration(ctx context.Context, id string, input domaindocker.HostInput, fields []string) (domaindocker.Host, error) {
	item := hostFromInput(input)
	values := map[string]any{"name": item.Name, "endpoint": item.Endpoint, "environment": item.Environment, "owner": item.Owner, "team": item.Team, "virtualizationConnectionId": item.VirtualizationConnectionID, "vmId": item.VMID, "vmName": item.VMName, "availablePortStart": item.AvailablePortStart, "availablePortEnd": item.AvailablePortEnd, "labels": item.Labels, "config": item.Config}
	if err := r.patchConfigurationFields(ctx, "docker_hosts", id, values, fields); err != nil {
		return domaindocker.Host{}, err
	}
	return r.GetHost(ctx, id)
}

func (r *Repository) PatchProjectConfiguration(ctx context.Context, id string, input domaindocker.ProjectInput, fields, names []string) (domaindocker.Project, error) {
	var result domaindocker.Project
	err := dbtx.Within(ctx, r.db, func(txctx context.Context) error {
		var err error
		result, err = r.patchProjectConfiguration(txctx, id, input, fields, names)
		return err
	})
	return result, err
}

func (r *Repository) patchProjectConfiguration(ctx context.Context, id string, input domaindocker.ProjectInput, fields, names []string) (domaindocker.Project, error) {
	current, err := r.lockProject(ctx, id)
	if err != nil {
		return domaindocker.Project{}, err
	}
	item := projectFromInput(input)
	moving := item.HostID != current.HostID
	if moving || containsConfigurationField(fields, "slug") && item.Slug != current.Slug {
		if err := r.checkProjectHostMove(ctx, current); err != nil {
			return domaindocker.Project{}, err
		}
	}
	values := map[string]any{"hostId": item.HostID, "name": item.Name, "slug": item.Slug, "description": item.Description, "environment": item.Environment, "owner": item.Owner, "team": item.Team, "sourceKind": item.SourceKind, "sourceRef": item.SourceRef, "composeContent": item.ComposeContent, "envContent": item.EnvContent, "templateId": item.TemplateID, "ttlSeconds": item.TTLSeconds, "labels": item.Labels, "config": item.Config, "expires_at": item.ExpiresAt}
	if err := r.patchConfigurationFields(ctx, "docker_projects", id, values, fields); err != nil {
		return domaindocker.Project{}, err
	}
	project, err := r.GetProject(ctx, id)
	if err != nil {
		return project, err
	}
	if moving {
		if err := dbtx.DB(ctx, r.db).Exec("UPDATE docker_services SET host_id=?,updated_at=? WHERE project_id=? AND ("+unobservedServiceDefinition+")", project.HostID, time.Now().UTC(), project.ID).Error; err != nil {
			return project, err
		}
	}
	if moving || containsConfigurationField(fields, "composeContent") {
		if err := r.syncProjectDefinitions(ctx, project, names); err != nil {
			return project, err
		}
	}
	return project, nil
}

var configurationColumns = map[string]string{
	"name": "name", "endpoint": "endpoint", "environment": "environment", "owner": "owner", "team": "team",
	"virtualizationConnectionId": "virtualization_connection_id", "vmId": "vm_id", "vmName": "vm_name",
	"availablePortStart": "available_port_start", "availablePortEnd": "available_port_end", "labels": "labels", "config": "config",
	"hostId": "host_id", "slug": "slug", "description": "description", "sourceKind": "source_kind", "sourceRef": "source_ref",
	"composeContent": "compose_content", "envContent": "env_content", "templateId": "template_id", "ttlSeconds": "ttl_seconds",
}

func (r *Repository) patchConfigurationFields(ctx context.Context, table, id string, values map[string]any, fields []string) error {
	if table != "docker_hosts" && table != "docker_projects" || len(fields) == 0 {
		return apperrors.ErrInvalidArgument
	}
	updates := map[string]any{"updated_at": time.Now().UTC()}
	for _, field := range fields {
		column, known := configurationColumns[field]
		value, supported := values[field]
		if !known || !supported {
			return apperrors.ErrInvalidArgument
		}
		if field == "labels" || field == "config" {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			value = gorm.Expr("?::jsonb", string(raw))
		}
		updates[column] = value
		if field == "ttlSeconds" {
			updates["expires_at"] = values["expires_at"]
		}
	}
	result := dbtx.DB(ctx, r.db).Table(table).Where("id=?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrNotFound
	}
	return nil
}

func containsConfigurationField(fields []string, target string) bool {
	for _, field := range fields {
		if field == target {
			return true
		}
	}
	return false
}
