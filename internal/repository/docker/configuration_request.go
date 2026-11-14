package docker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) GetConfigurationRequest(ctx context.Context, actor, key string) (domaindocker.ConfigurationRequest, error) {
	var item domaindocker.ConfigurationRequest
	var raw []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT id,actor_id,request_key,input_digest,resource_kind,resource_id,action,receipt FROM docker_configuration_requests WHERE actor_id=? AND request_key=?`, actor, key).Row().Scan(&item.ID, &item.ActorID, &item.Key, &item.InputDigest, &item.Kind, &item.ResourceID, &item.Action, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if json.Unmarshal(raw, &item.Receipt) != nil {
		return item, apperrors.ErrServiceUnavailable
	}
	return item, nil
}

func (r *Repository) ApplyConfigurationRequest(ctx context.Context, request domaindocker.ConfigurationRequest, apply func(context.Context) (sohaapi.DockerResourceIdentity, error)) (domaindocker.ConfigurationRequest, error) {
	var result domaindocker.ConfigurationRequest
	err := dbtx.Within(ctx, r.db, func(txctx context.Context) error {
		if err := dbtx.DB(txctx, r.db).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "docker.configuration:"+request.ID).Error; err != nil {
			return err
		}
		previous, err := r.GetConfigurationRequest(txctx, request.ActorID, request.Key)
		if err == nil {
			if previous.InputDigest != request.InputDigest {
				return apperrors.ErrConflict
			}
			result = previous
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if request.Action != "register" {
			if err := r.lockConfigurationTarget(txctx, request); err != nil {
				return err
			}
		}
		identity, err := apply(txctx)
		if err != nil {
			return err
		}
		if identity.ID != request.ResourceID || string(identity.ResourceKind) != request.Kind || identity.UID == uuid.Nil || identity.ConfigurationRevision == uuid.Nil {
			return apperrors.ErrServiceUnavailable
		}
		request.Receipt = sohaapi.DockerConfigurationReceipt{ID: request.ID, RequestKey: request.Key, Action: sohaapi.DockerConfigurationReceiptAction(request.Action), Resource: identity, Status: "committed", Deleted: request.Action == "delete", CreatedAt: time.Now().UTC()}
		raw, err := json.Marshal(request.Receipt)
		if err != nil {
			return err
		}
		err = dbtx.DB(txctx, r.db).Exec(`INSERT INTO docker_configuration_requests(id,actor_id,request_key,input_digest,resource_kind,resource_id,action,receipt) VALUES (?,?,?,?,?,?,?,?::jsonb)`, request.ID, request.ActorID, request.Key, request.InputDigest, request.Kind, request.ResourceID, request.Action, string(raw)).Error
		result = request
		return err
	})
	return result, err
}

func (r *Repository) lockConfigurationTarget(ctx context.Context, input domaindocker.ConfigurationRequest) error {
	if input.Kind == "docker.host" {
		return r.lockHostConfigurationTarget(ctx, input)
	}
	if input.Kind != "docker.project" {
		return apperrors.ErrInvalidArgument
	}
	return r.lockProjectConfigurationTarget(ctx, input)
}

func (r *Repository) lockHostConfigurationTarget(ctx context.Context, input domaindocker.ConfigurationRequest) error {
	host, err := scanHostRow(dbtx.DB(ctx, r.db).Raw(hostSelect()+" WHERE id=? FOR UPDATE", input.ResourceID).Row())
	if err != nil {
		return err
	}
	if host.ResourceUID != input.ExpectedUID || host.ConfigurationRevision != input.ExpectedRevision {
		return apperrors.ErrConflict
	}
	if input.Action == "delete" {
		return r.checkHostDeregistration(ctx, host.ID)
	}
	return nil
}

func (r *Repository) lockProjectConfigurationTarget(ctx context.Context, input domaindocker.ConfigurationRequest) error {
	// Lock parents in a stable order before the child. A concurrent host move
	// changes the project revision and is rejected after locking the child.
	observed, err := r.GetProject(ctx, input.ResourceID)
	if err != nil {
		return err
	}
	hosts := []string{observed.HostID}
	if input.TargetHostID != "" && input.TargetHostID != observed.HostID {
		hosts = append(hosts, input.TargetHostID)
	}
	sort.Strings(hosts)
	for _, id := range hosts {
		host, err := scanHostRow(dbtx.DB(ctx, r.db).Raw(hostSelect()+" WHERE id=? FOR UPDATE", id).Row())
		if err != nil {
			return err
		}
		if id == input.TargetHostID && host.ResourceUID != input.TargetHostUID {
			return apperrors.ErrConflict
		}
	}
	project, err := r.lockProject(ctx, input.ResourceID)
	if err != nil {
		return err
	}
	if project.HostID != observed.HostID || project.ResourceUID != input.ExpectedUID || project.ConfigurationRevision != input.ExpectedRevision {
		return apperrors.ErrConflict
	}
	if input.Action == "delete" {
		return r.checkProjectHostMove(ctx, project)
	}
	return nil
}

func (r *Repository) checkHostDeregistration(ctx context.Context, id string) error {
	var blocked bool
	err := dbtx.DB(ctx, r.db).Raw(`SELECT EXISTS(SELECT 1 FROM docker_projects WHERE host_id=?) OR EXISTS(SELECT 1 FROM docker_services WHERE host_id=?) OR EXISTS(SELECT 1 FROM docker_port_mappings WHERE host_id=?) OR EXISTS(SELECT 1 FROM docker_operations WHERE host_id=?)`, id, id, id, id).Scan(&blocked).Error
	if err != nil {
		return err
	}
	if blocked {
		return fmt.Errorf("%w: unregister child metadata and resolve runtime evidence/operations before removing this host", apperrors.ErrConflict)
	}
	return nil
}
