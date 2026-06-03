package docker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaindocker "github.com/soha/soha/internal/domain/docker"
	"github.com/soha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

var ErrNotFound = fmt.Errorf("%w: docker record not found", apperrors.ErrNotFound)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListHosts(ctx context.Context, filter domaindocker.HostFilter) ([]domaindocker.Host, error) {
	query := hostSelect()
	clauses, args := hostClauses(filter)
	query = appendWhere(query, clauses)
	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query docker hosts: %w", err)
	}
	defer rows.Close()
	items := make([]domaindocker.Host, 0, limit)
	for rows.Next() {
		item, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountHosts(ctx context.Context, filter domaindocker.HostFilter) (int, error) {
	clauses, args := hostClauses(filter)
	return r.count(ctx, "docker_hosts", clauses, args)
}

func (r *Repository) GetHost(ctx context.Context, id string) (domaindocker.Host, error) {
	row := r.db.WithContext(ctx).Raw(hostSelect()+" WHERE id = ? LIMIT 1", strings.TrimSpace(id)).Row()
	return scanHostRow(row)
}

func (r *Repository) CreateHost(ctx context.Context, input domaindocker.HostInput) (domaindocker.Host, error) {
	item := hostFromInput(input)
	labels, err := marshalJSON(item.Labels)
	if err != nil {
		return domaindocker.Host{}, fmt.Errorf("marshal docker host labels: %w", err)
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.Host{}, fmt.Errorf("marshal docker host config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO docker_hosts (
			id, name, status, endpoint, agent_id, agent_version, docker_version, compose_version,
			architecture, environment, owner, team, virtualization_connection_id, vm_id, vm_name, ip_address,
			cpu_core_count, memory_bytes, disk_bytes, available_port_start, available_port_end,
			labels, config, last_heartbeat_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?)
	`, item.ID, item.Name, item.Status, nullableString(item.Endpoint), nullableString(item.AgentID), nullableString(item.AgentVersion),
		nullableString(item.DockerVersion), nullableString(item.ComposeVersion), nullableString(item.Architecture), nullableString(item.Environment), nullableString(item.Owner),
		nullableString(item.Team), nullableString(item.VirtualizationConnectionID), nullableString(item.VMID), nullableString(item.VMName),
		nullableString(item.IPAddress), item.CPUCoreCount, item.MemoryBytes, item.DiskBytes, item.AvailablePortStart, item.AvailablePortEnd,
		string(labels), string(config), item.LastHeartbeatAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindocker.Host{}, fmt.Errorf("create docker host: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateHost(ctx context.Context, id string, input domaindocker.HostInput) (domaindocker.Host, error) {
	item := hostFromInput(input)
	item.ID = strings.TrimSpace(id)
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "docker_hosts", item.ID)
	labels, err := marshalJSON(item.Labels)
	if err != nil {
		return domaindocker.Host{}, fmt.Errorf("marshal docker host labels: %w", err)
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.Host{}, fmt.Errorf("marshal docker host config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE docker_hosts
		SET name = ?, status = ?, endpoint = ?, agent_id = ?, agent_version = ?, docker_version = ?,
			compose_version = ?, architecture = ?, environment = ?, owner = ?, team = ?, virtualization_connection_id = ?,
			vm_id = ?, vm_name = ?, ip_address = ?, cpu_core_count = ?, memory_bytes = ?, disk_bytes = ?,
			available_port_start = ?, available_port_end = ?, labels = ?::jsonb, config = ?::jsonb, updated_at = ?
		WHERE id = ?
	`, item.Name, item.Status, nullableString(item.Endpoint), nullableString(item.AgentID), nullableString(item.AgentVersion),
		nullableString(item.DockerVersion), nullableString(item.ComposeVersion), nullableString(item.Architecture), nullableString(item.Environment), nullableString(item.Owner),
		nullableString(item.Team), nullableString(item.VirtualizationConnectionID), nullableString(item.VMID), nullableString(item.VMName),
		nullableString(item.IPAddress), item.CPUCoreCount, item.MemoryBytes, item.DiskBytes, item.AvailablePortStart, item.AvailablePortEnd,
		string(labels), string(config), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindocker.Host{}, fmt.Errorf("update docker host: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindocker.Host{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) TouchHostRuntime(ctx context.Context, id string, input domaindocker.HostInput) (domaindocker.Host, error) {
	item, err := r.GetHost(ctx, id)
	if err != nil {
		return domaindocker.Host{}, err
	}
	if value := strings.TrimSpace(input.Status); value != "" {
		item.Status = value
	}
	if value := strings.TrimSpace(input.Endpoint); value != "" {
		item.Endpoint = value
	}
	if value := strings.TrimSpace(input.AgentID); value != "" {
		item.AgentID = value
	}
	if value := strings.TrimSpace(input.AgentVersion); value != "" {
		item.AgentVersion = value
	}
	if value := strings.TrimSpace(input.DockerVersion); value != "" {
		item.DockerVersion = value
	}
	if value := strings.TrimSpace(input.ComposeVersion); value != "" {
		item.ComposeVersion = value
	}
	if value := strings.TrimSpace(input.Architecture); value != "" {
		item.Architecture = value
	}
	if value := strings.TrimSpace(input.IPAddress); value != "" {
		item.IPAddress = value
	}
	if value := strings.TrimSpace(input.VMID); value != "" {
		item.VMID = value
	}
	if value := strings.TrimSpace(input.VMName); value != "" {
		item.VMName = value
	}
	if len(input.Config) > 0 {
		item.Config = mergeJSONMap(item.Config, input.Config)
	}
	now := time.Now().UTC()
	item.LastHeartbeatAt = &now
	item.UpdatedAt = now
	labels, err := marshalJSON(item.Labels)
	if err != nil {
		return domaindocker.Host{}, fmt.Errorf("marshal docker host labels: %w", err)
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.Host{}, fmt.Errorf("marshal docker host config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE docker_hosts
		SET status = ?, endpoint = ?, agent_id = ?, agent_version = ?, docker_version = ?, compose_version = ?,
			architecture = ?, ip_address = ?, vm_id = ?, vm_name = ?, labels = ?::jsonb, config = ?::jsonb, last_heartbeat_at = ?, updated_at = ?
		WHERE id = ?
	`, item.Status, nullableString(item.Endpoint), nullableString(item.AgentID), nullableString(item.AgentVersion),
		nullableString(item.DockerVersion), nullableString(item.ComposeVersion), nullableString(item.Architecture), nullableString(item.IPAddress),
		nullableString(item.VMID), nullableString(item.VMName), string(labels), string(config), item.LastHeartbeatAt,
		item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindocker.Host{}, fmt.Errorf("touch docker host runtime: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindocker.Host{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) DeleteHost(ctx context.Context, id string) error {
	return r.deleteByID(ctx, "docker_hosts", id, "delete docker host")
}

func (r *Repository) ListProjects(ctx context.Context, filter domaindocker.ProjectFilter) ([]domaindocker.Project, error) {
	query := projectSelect()
	clauses, args := projectClauses(filter)
	query = appendWhere(query, clauses)
	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query docker projects: %w", err)
	}
	defer rows.Close()
	items := make([]domaindocker.Project, 0, limit)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountProjects(ctx context.Context, filter domaindocker.ProjectFilter) (int, error) {
	clauses, args := projectClauses(filter)
	return r.count(ctx, "docker_projects", clauses, args)
}

func (r *Repository) GetProject(ctx context.Context, id string) (domaindocker.Project, error) {
	row := r.db.WithContext(ctx).Raw(projectSelect()+" WHERE id = ? LIMIT 1", strings.TrimSpace(id)).Row()
	return scanProjectRow(row)
}

func (r *Repository) CreateProject(ctx context.Context, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	item := projectFromInput(input)
	labels, err := marshalJSON(item.Labels)
	if err != nil {
		return domaindocker.Project{}, fmt.Errorf("marshal docker project labels: %w", err)
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.Project{}, fmt.Errorf("marshal docker project config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO docker_projects (
			id, host_id, name, slug, description, environment, owner, team, source_kind, source_ref,
			compose_content, env_content, status, desired_state, template_id, ttl_seconds, expires_at,
			last_deployed_at, labels, config, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?)
	`, item.ID, item.HostID, item.Name, item.Slug, nullableString(item.Description), nullableString(item.Environment),
		nullableString(item.Owner), nullableString(item.Team), nullableString(item.SourceKind), nullableString(item.SourceRef),
		nullableString(item.ComposeContent), nullableString(item.EnvContent), item.Status, nullableString(item.DesiredState),
		nullableString(item.TemplateID), item.TTLSeconds, item.ExpiresAt, item.LastDeployedAt, string(labels), string(config),
		item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindocker.Project{}, fmt.Errorf("create docker project: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateProject(ctx context.Context, id string, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	item := projectFromInput(input)
	item.ID = strings.TrimSpace(id)
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "docker_projects", item.ID)
	labels, err := marshalJSON(item.Labels)
	if err != nil {
		return domaindocker.Project{}, fmt.Errorf("marshal docker project labels: %w", err)
	}
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.Project{}, fmt.Errorf("marshal docker project config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE docker_projects
		SET host_id = ?, name = ?, slug = ?, description = ?, environment = ?, owner = ?, team = ?,
			source_kind = ?, source_ref = ?, compose_content = ?, env_content = ?, status = ?, desired_state = ?,
			template_id = ?, ttl_seconds = ?, expires_at = ?, labels = ?::jsonb, config = ?::jsonb, updated_at = ?
		WHERE id = ?
	`, item.HostID, item.Name, item.Slug, nullableString(item.Description), nullableString(item.Environment),
		nullableString(item.Owner), nullableString(item.Team), nullableString(item.SourceKind), nullableString(item.SourceRef),
		nullableString(item.ComposeContent), nullableString(item.EnvContent), item.Status, nullableString(item.DesiredState),
		nullableString(item.TemplateID), item.TTLSeconds, item.ExpiresAt, string(labels), string(config), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindocker.Project{}, fmt.Errorf("update docker project: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindocker.Project{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) UpdateProjectRuntime(ctx context.Context, id string, status string, desiredState string, lastDeployedAt *time.Time) (domaindocker.Project, error) {
	fields := []string{"updated_at = ?"}
	args := []any{time.Now().UTC()}
	if value := strings.TrimSpace(status); value != "" {
		fields = append(fields, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(desiredState); value != "" {
		fields = append(fields, "desired_state = ?")
		args = append(args, value)
	}
	if lastDeployedAt != nil {
		fields = append(fields, "last_deployed_at = ?")
		args = append(args, lastDeployedAt)
	}
	args = append(args, strings.TrimSpace(id))
	result := r.db.WithContext(ctx).Exec("UPDATE docker_projects SET "+strings.Join(fields, ", ")+" WHERE id = ?", args...)
	if result.Error != nil {
		return domaindocker.Project{}, fmt.Errorf("update docker project runtime: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindocker.Project{}, ErrNotFound
	}
	return r.GetProject(ctx, id)
}

func (r *Repository) DeleteProject(ctx context.Context, id string) error {
	return r.deleteByID(ctx, "docker_projects", id, "delete docker project")
}

func (r *Repository) ListServices(ctx context.Context, filter domaindocker.ServiceFilter) ([]domaindocker.Service, error) {
	query := serviceSelect()
	clauses, args := serviceClauses(filter)
	query = appendWhere(query, clauses)
	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query docker services: %w", err)
	}
	defer rows.Close()
	items := make([]domaindocker.Service, 0, limit)
	for rows.Next() {
		item, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountServices(ctx context.Context, filter domaindocker.ServiceFilter) (int, error) {
	clauses, args := serviceClauses(filter)
	return r.count(ctx, "docker_services", clauses, args)
}

func (r *Repository) GetService(ctx context.Context, id string) (domaindocker.Service, error) {
	row := r.db.WithContext(ctx).Raw(serviceSelect()+" WHERE id = ? LIMIT 1", strings.TrimSpace(id)).Row()
	return scanServiceRow(row)
}

func (r *Repository) UpsertService(ctx context.Context, input domaindocker.ServiceInput) (domaindocker.Service, error) {
	item := serviceFromInput(input)
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.Service{}, fmt.Errorf("marshal docker service config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO docker_services (
			id, project_id, host_id, name, image, status, container_id, restart_count, cpu_percent,
			memory_bytes, network_rx_bytes, network_tx_bytes, config, last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?)
		ON CONFLICT (project_id, name) DO UPDATE SET
			host_id = EXCLUDED.host_id,
			image = EXCLUDED.image,
			status = EXCLUDED.status,
			container_id = EXCLUDED.container_id,
			restart_count = EXCLUDED.restart_count,
			cpu_percent = EXCLUDED.cpu_percent,
			memory_bytes = EXCLUDED.memory_bytes,
			network_rx_bytes = EXCLUDED.network_rx_bytes,
			network_tx_bytes = EXCLUDED.network_tx_bytes,
			config = EXCLUDED.config,
			last_seen_at = EXCLUDED.last_seen_at,
			updated_at = EXCLUDED.updated_at
	`, item.ID, item.ProjectID, item.HostID, item.Name, nullableString(item.Image), item.Status, nullableString(item.ContainerID),
		item.RestartCount, item.CPUPercent, item.MemoryBytes, item.NetworkRxBytes, item.NetworkTxBytes, string(config),
		item.LastSeenAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindocker.Service{}, fmt.Errorf("upsert docker service: %w", err)
	}
	stored, err := r.getServiceByProjectName(ctx, item.ProjectID, item.Name)
	if err != nil {
		return item, nil
	}
	return stored, nil
}

func (r *Repository) DeleteService(ctx context.Context, id string) error {
	return r.deleteByID(ctx, "docker_services", id, "delete docker service")
}

func (r *Repository) ListPortMappings(ctx context.Context, filter domaindocker.PortMappingFilter) ([]domaindocker.PortMapping, error) {
	query := portMappingSelect()
	clauses, args := portMappingClauses(filter)
	query = appendWhere(query, clauses)
	query += " ORDER BY host_port ASC, updated_at DESC LIMIT ? OFFSET ?"
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query docker port mappings: %w", err)
	}
	defer rows.Close()
	items := make([]domaindocker.PortMapping, 0, limit)
	for rows.Next() {
		item, err := scanPortMapping(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountPortMappings(ctx context.Context, filter domaindocker.PortMappingFilter) (int, error) {
	clauses, args := portMappingClauses(filter)
	return r.count(ctx, "docker_port_mappings", clauses, args)
}

func (r *Repository) GetPortMapping(ctx context.Context, id string) (domaindocker.PortMapping, error) {
	row := r.db.WithContext(ctx).Raw(portMappingSelect()+" WHERE id = ? LIMIT 1", strings.TrimSpace(id)).Row()
	return scanPortMappingRow(row)
}

func (r *Repository) CreatePortMapping(ctx context.Context, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	item := portMappingFromInput(input)
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.PortMapping{}, fmt.Errorf("marshal docker port mapping config: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO docker_port_mappings (
			id, host_id, project_id, service_id, name, host_ip, host_port, container_port, protocol,
			exposure_scope, status, domain_name, domain_scheme, domain_tls_enabled, access_url, owner, expires_at,
			config, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?)
	`, item.ID, item.HostID, nullableString(item.ProjectID), nullableString(item.ServiceID), item.Name, nullableString(item.HostIP),
		item.HostPort, item.ContainerPort, item.Protocol, item.ExposureScope, item.Status, nullableString(item.DomainName),
		nullableString(item.DomainScheme), item.DomainTLSEnabled, nullableString(item.AccessURL), nullableString(item.Owner),
		item.ExpiresAt, string(config), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindocker.PortMapping{}, fmt.Errorf("create docker port mapping: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdatePortMapping(ctx context.Context, id string, input domaindocker.PortMappingInput) (domaindocker.PortMapping, error) {
	item := portMappingFromInput(input)
	item.ID = strings.TrimSpace(id)
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "docker_port_mappings", item.ID)
	config, err := marshalJSON(item.Config)
	if err != nil {
		return domaindocker.PortMapping{}, fmt.Errorf("marshal docker port mapping config: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE docker_port_mappings
		SET host_id = ?, project_id = ?, service_id = ?, name = ?, host_ip = ?, host_port = ?,
			container_port = ?, protocol = ?, exposure_scope = ?, status = ?, domain_name = ?,
			domain_scheme = ?, domain_tls_enabled = ?, access_url = ?, owner = ?, expires_at = ?,
			config = ?::jsonb, updated_at = ?
		WHERE id = ?
	`, item.HostID, nullableString(item.ProjectID), nullableString(item.ServiceID), item.Name, nullableString(item.HostIP),
		item.HostPort, item.ContainerPort, item.Protocol, item.ExposureScope, item.Status, nullableString(item.DomainName),
		nullableString(item.DomainScheme), item.DomainTLSEnabled, nullableString(item.AccessURL), nullableString(item.Owner),
		item.ExpiresAt, string(config), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindocker.PortMapping{}, fmt.Errorf("update docker port mapping: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindocker.PortMapping{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) DeletePortMapping(ctx context.Context, id string) error {
	return r.deleteByID(ctx, "docker_port_mappings", id, "delete docker port mapping")
}

func (r *Repository) CreateContainerStart(ctx context.Context, input domaindocker.ContainerStartCreateInput) (domaindocker.ContainerStartCreateResult, error) {
	var result domaindocker.ContainerStartCreateResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := &Repository{db: tx}
		project, err := txRepo.CreateProject(ctx, input.Project)
		if err != nil {
			return err
		}
		serviceInput := input.Service
		serviceInput.ProjectID = project.ID
		if serviceInput.HostID == "" {
			serviceInput.HostID = project.HostID
		}
		service, err := txRepo.UpsertService(ctx, serviceInput)
		if err != nil {
			return err
		}
		portInputs := append([]domaindocker.PortMappingInput(nil), input.PortMappings...)
		if len(portInputs) == 0 {
			portInputs = append(portInputs, input.PortMapping)
		}
		ports := make([]domaindocker.PortMapping, 0, len(portInputs))
		for _, portInput := range portInputs {
			portInput.ProjectID = project.ID
			portInput.ServiceID = service.ID
			if portInput.HostID == "" {
				portInput.HostID = project.HostID
			}
			port, err := txRepo.CreatePortMapping(ctx, portInput)
			if err != nil {
				return err
			}
			ports = append(ports, port)
		}
		operationInput := input.Operation
		operationInput.ProjectID = project.ID
		operationInput.ServiceID = service.ID
		if operationInput.HostID == "" {
			operationInput.HostID = project.HostID
		}
		operation, err := txRepo.CreateOperation(ctx, operationInput)
		if err != nil {
			return err
		}
		result = domaindocker.ContainerStartCreateResult{
			Project:      project,
			Service:      service,
			PortMappings: ports,
			Operation:    operation,
		}
		if len(ports) > 0 {
			result.PortMapping = ports[0]
		}
		return nil
	})
	if err != nil {
		return domaindocker.ContainerStartCreateResult{}, fmt.Errorf("create docker container start records: %w", err)
	}
	return result, nil
}

func (r *Repository) ListTemplates(ctx context.Context, filter domaindocker.TemplateFilter) ([]domaindocker.Template, error) {
	query := templateSelect()
	clauses, args := templateClauses(filter)
	query = appendWhere(query, clauses)
	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query docker templates: %w", err)
	}
	defer rows.Close()
	items := make([]domaindocker.Template, 0, limit)
	for rows.Next() {
		item, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountTemplates(ctx context.Context, filter domaindocker.TemplateFilter) (int, error) {
	clauses, args := templateClauses(filter)
	return r.count(ctx, "docker_templates", clauses, args)
}

func (r *Repository) GetTemplate(ctx context.Context, id string) (domaindocker.Template, error) {
	row := r.db.WithContext(ctx).Raw(templateSelect()+" WHERE id = ? LIMIT 1", strings.TrimSpace(id)).Row()
	return scanTemplateRow(row)
}

func (r *Repository) CreateTemplate(ctx context.Context, input domaindocker.TemplateInput) (domaindocker.Template, error) {
	item := templateFromInput(input)
	variables, err := marshalJSON(item.Variables)
	if err != nil {
		return domaindocker.Template{}, fmt.Errorf("marshal docker template variables: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO docker_templates (id, name, description, template_kind, compose_content, env_content, variables, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?)
	`, item.ID, item.Name, nullableString(item.Description), item.TemplateKind, nullableString(item.ComposeContent),
		nullableString(item.EnvContent), string(variables), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindocker.Template{}, fmt.Errorf("create docker template: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateTemplate(ctx context.Context, id string, input domaindocker.TemplateInput) (domaindocker.Template, error) {
	item := templateFromInput(input)
	item.ID = strings.TrimSpace(id)
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "docker_templates", item.ID)
	variables, err := marshalJSON(item.Variables)
	if err != nil {
		return domaindocker.Template{}, fmt.Errorf("marshal docker template variables: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE docker_templates
		SET name = ?, description = ?, template_kind = ?, compose_content = ?, env_content = ?,
			variables = ?::jsonb, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Name, nullableString(item.Description), item.TemplateKind, nullableString(item.ComposeContent),
		nullableString(item.EnvContent), string(variables), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindocker.Template{}, fmt.Errorf("update docker template: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindocker.Template{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) DeleteTemplate(ctx context.Context, id string) error {
	return r.deleteByID(ctx, "docker_templates", id, "delete docker template")
}

func (r *Repository) CreateOperation(ctx context.Context, input domaindocker.OperationInput) (domaindocker.Operation, error) {
	item := operationFromInput(input)
	payload, err := marshalJSON(item.Payload)
	if err != nil {
		return domaindocker.Operation{}, fmt.Errorf("marshal docker operation payload: %w", err)
	}
	result, err := marshalJSON(item.Result)
	if err != nil {
		return domaindocker.Operation{}, fmt.Errorf("marshal docker operation result: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO docker_operations (
			id, host_id, project_id, service_id, operation_kind, status, requested_by, claimed_by_worker_id,
			attempt_count, max_retries, timeout_seconds, payload, result, started_at, last_heartbeat_at,
			finished_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?)
	`, item.ID, nullableString(item.HostID), nullableString(item.ProjectID), nullableString(item.ServiceID), item.OperationKind,
		item.Status, nullableString(item.RequestedBy), nullableString(item.ClaimedByWorkerID), item.AttemptCount, item.MaxRetries,
		item.TimeoutSeconds, string(payload), string(result), item.StartedAt, item.LastHeartbeatAt, item.FinishedAt,
		item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindocker.Operation{}, fmt.Errorf("create docker operation: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateOperation(ctx context.Context, item domaindocker.Operation) (domaindocker.Operation, error) {
	payload, err := marshalJSON(item.Payload)
	if err != nil {
		return domaindocker.Operation{}, fmt.Errorf("marshal docker operation payload: %w", err)
	}
	resultPayload, err := marshalJSON(item.Result)
	if err != nil {
		return domaindocker.Operation{}, fmt.Errorf("marshal docker operation result: %w", err)
	}
	item.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE docker_operations
		SET host_id = ?, project_id = ?, service_id = ?, operation_kind = ?, status = ?, requested_by = ?,
			claimed_by_worker_id = ?, attempt_count = ?, max_retries = ?, timeout_seconds = ?,
			payload = ?::jsonb, result = ?::jsonb, started_at = ?, last_heartbeat_at = ?, finished_at = ?, updated_at = ?
		WHERE id = ?
	`, nullableString(item.HostID), nullableString(item.ProjectID), nullableString(item.ServiceID), item.OperationKind, item.Status,
		nullableString(item.RequestedBy), nullableString(item.ClaimedByWorkerID), item.AttemptCount, item.MaxRetries,
		item.TimeoutSeconds, string(payload), string(resultPayload), item.StartedAt, item.LastHeartbeatAt, item.FinishedAt,
		item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindocker.Operation{}, fmt.Errorf("update docker operation: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindocker.Operation{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) ClaimOperation(ctx context.Context, workerID string, agentID string, hostIDs []string, operationKinds []string, now time.Time) (domaindocker.Operation, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return domaindocker.Operation{}, ErrNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var task domaindocker.Operation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		clauses := []string{"status = 'queued'"}
		args := []any{}
		if values := compactStrings(hostIDs); len(values) > 0 {
			clauses = append(clauses, fmt.Sprintf("host_id IN (%s)", placeholders(len(values))))
			for _, value := range values {
				args = append(args, value)
			}
		}
		if values := compactStrings(operationKinds); len(values) > 0 {
			clauses = append(clauses, fmt.Sprintf("operation_kind IN (%s)", placeholders(len(values))))
			for _, value := range values {
				args = append(args, value)
			}
		}
		rows, queryErr := tx.Raw(operationSelect()+" WHERE "+strings.Join(clauses, " AND ")+`
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		`, args...).Rows()
		if queryErr != nil {
			return fmt.Errorf("claim docker operation query: %w", queryErr)
		}
		if !rows.Next() {
			_ = rows.Close()
			return ErrNotFound
		}
		item, scanErr := scanOperation(rows)
		closeErr := rows.Close()
		if scanErr != nil {
			return scanErr
		}
		if closeErr != nil {
			return fmt.Errorf("close claimed docker operation rows: %w", closeErr)
		}
		item.Status = "running"
		item.ClaimedByWorkerID = workerID
		item.AttemptCount++
		item.StartedAt = &now
		item.LastHeartbeatAt = &now
		item.FinishedAt = nil
		item.UpdatedAt = now
		item.Result = mergeJSONMap(item.Result, map[string]any{
			"claimedByWorkerId": workerID,
			"claimedByAgentId":  strings.TrimSpace(agentID),
			"claimedAt":         now.Format(time.RFC3339),
		})
		payload, marshalErr := marshalJSON(item.Payload)
		if marshalErr != nil {
			return fmt.Errorf("marshal claimed docker operation payload: %w", marshalErr)
		}
		resultPayload, marshalErr := marshalJSON(item.Result)
		if marshalErr != nil {
			return fmt.Errorf("marshal claimed docker operation result: %w", marshalErr)
		}
		update := tx.Exec(`
			UPDATE docker_operations
			SET status = ?, claimed_by_worker_id = ?, attempt_count = ?, max_retries = ?, timeout_seconds = ?,
				payload = ?::jsonb, result = ?::jsonb, started_at = ?, last_heartbeat_at = ?, finished_at = ?, updated_at = ?
			WHERE id = ? AND status = 'queued'
		`, item.Status, nullableString(item.ClaimedByWorkerID), item.AttemptCount, item.MaxRetries, item.TimeoutSeconds,
			string(payload), string(resultPayload), item.StartedAt, item.LastHeartbeatAt, item.FinishedAt, item.UpdatedAt, item.ID)
		if update.Error != nil {
			return fmt.Errorf("claim docker operation update: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			return ErrNotFound
		}
		task = item
		return nil
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return domaindocker.Operation{}, ErrNotFound
	}
	if err != nil {
		return domaindocker.Operation{}, err
	}
	return task, nil
}

func (r *Repository) GetOperation(ctx context.Context, id string) (domaindocker.Operation, error) {
	row := r.db.WithContext(ctx).Raw(operationSelect()+" WHERE id = ? LIMIT 1", strings.TrimSpace(id)).Row()
	return scanOperationRow(row)
}

func (r *Repository) ListOperations(ctx context.Context, filter domaindocker.OperationFilter) ([]domaindocker.Operation, error) {
	query := operationSelect()
	clauses, args := operationClauses(filter)
	query = appendWhere(query, clauses)
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	limit, offset := limitOffset(filter.Limit, filter.Page, filter.PageSize)
	args = append(args, limit, offset)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query docker operations: %w", err)
	}
	defer rows.Close()
	items := make([]domaindocker.Operation, 0, limit)
	for rows.Next() {
		item, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CountOperations(ctx context.Context, filter domaindocker.OperationFilter) (int, error) {
	clauses, args := operationClauses(filter)
	return r.count(ctx, "docker_operations", clauses, args)
}

func (r *Repository) CreateOperationLog(ctx context.Context, item domaindocker.OperationLog) error {
	if strings.TrimSpace(item.ID) == "" {
		item.ID = uuid.NewString()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	payload, err := marshalJSON(item.Payload)
	if err != nil {
		return fmt.Errorf("marshal docker operation log payload: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO docker_operation_logs (id, operation_id, log_level, message, payload, created_at)
		VALUES (?, ?, ?, ?, ?::jsonb, ?)
	`, item.ID, item.OperationID, item.LogLevel, item.Message, string(payload), item.CreatedAt).Error
}

func (r *Repository) ListOperationLogs(ctx context.Context, operationID string, limit int) ([]domaindocker.OperationLog, error) {
	normalized := normalizedLimit(limit)
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, operation_id, log_level, message, payload, created_at
		FROM docker_operation_logs
		WHERE operation_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, strings.TrimSpace(operationID), normalized).Rows()
	if err != nil {
		return nil, fmt.Errorf("query docker operation logs: %w", err)
	}
	defer rows.Close()
	items := make([]domaindocker.OperationLog, 0, normalized)
	for rows.Next() {
		item, err := scanOperationLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func hostFromInput(input domaindocker.HostInput) domaindocker.Host {
	now := time.Now().UTC()
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "pending"
	}
	start, end := input.AvailablePortStart, input.AvailablePortEnd
	if start <= 0 {
		start = 20000
	}
	if end <= 0 {
		end = 39999
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domaindocker.Host{
		ID:                         id,
		Name:                       strings.TrimSpace(input.Name),
		Status:                     status,
		Endpoint:                   strings.TrimSpace(input.Endpoint),
		AgentID:                    strings.TrimSpace(input.AgentID),
		AgentVersion:               strings.TrimSpace(input.AgentVersion),
		DockerVersion:              strings.TrimSpace(input.DockerVersion),
		ComposeVersion:             strings.TrimSpace(input.ComposeVersion),
		Architecture:               strings.TrimSpace(input.Architecture),
		Environment:                strings.TrimSpace(input.Environment),
		Owner:                      strings.TrimSpace(input.Owner),
		Team:                       strings.TrimSpace(input.Team),
		VirtualizationConnectionID: strings.TrimSpace(input.VirtualizationConnectionID),
		VMID:                       strings.TrimSpace(input.VMID),
		VMName:                     strings.TrimSpace(input.VMName),
		IPAddress:                  strings.TrimSpace(input.IPAddress),
		CPUCoreCount:               input.CPUCoreCount,
		MemoryBytes:                input.MemoryBytes,
		DiskBytes:                  input.DiskBytes,
		AvailablePortStart:         start,
		AvailablePortEnd:           end,
		Labels:                     ensureMap(input.Labels),
		Config:                     ensureMap(input.Config),
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
}

func projectFromInput(input domaindocker.ProjectInput) domaindocker.Project {
	now := time.Now().UTC()
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "draft"
	}
	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		slug = slugify(input.Name)
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	var expiresAt *time.Time
	if input.TTLSeconds > 0 {
		t := now.Add(time.Duration(input.TTLSeconds) * time.Second)
		expiresAt = &t
	}
	return domaindocker.Project{
		ID:             id,
		HostID:         strings.TrimSpace(input.HostID),
		Name:           strings.TrimSpace(input.Name),
		Slug:           slug,
		Description:    strings.TrimSpace(input.Description),
		Environment:    strings.TrimSpace(input.Environment),
		Owner:          strings.TrimSpace(input.Owner),
		Team:           strings.TrimSpace(input.Team),
		SourceKind:     strings.TrimSpace(input.SourceKind),
		SourceRef:      strings.TrimSpace(input.SourceRef),
		ComposeContent: input.ComposeContent,
		EnvContent:     input.EnvContent,
		Status:         status,
		DesiredState:   strings.TrimSpace(input.DesiredState),
		TemplateID:     strings.TrimSpace(input.TemplateID),
		TTLSeconds:     input.TTLSeconds,
		ExpiresAt:      expiresAt,
		Labels:         ensureMap(input.Labels),
		Config:         ensureMap(input.Config),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func serviceFromInput(input domaindocker.ServiceInput) domaindocker.Service {
	now := time.Now().UTC()
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "unknown"
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	lastSeenAt := now
	return domaindocker.Service{
		ID:             id,
		ProjectID:      strings.TrimSpace(input.ProjectID),
		HostID:         strings.TrimSpace(input.HostID),
		Name:           strings.TrimSpace(input.Name),
		Image:          strings.TrimSpace(input.Image),
		Status:         status,
		ContainerID:    strings.TrimSpace(input.ContainerID),
		RestartCount:   input.RestartCount,
		CPUPercent:     input.CPUPercent,
		MemoryBytes:    input.MemoryBytes,
		NetworkRxBytes: input.NetworkRxBytes,
		NetworkTxBytes: input.NetworkTxBytes,
		Config:         ensureMap(input.Config),
		LastSeenAt:     &lastSeenAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func portMappingFromInput(input domaindocker.PortMappingInput) domaindocker.PortMapping {
	now := time.Now().UTC()
	protocol := strings.ToLower(strings.TrimSpace(input.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	exposureScope := strings.TrimSpace(input.ExposureScope)
	if exposureScope == "" {
		exposureScope = "internal"
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domaindocker.PortMapping{
		ID:               id,
		HostID:           strings.TrimSpace(input.HostID),
		ProjectID:        strings.TrimSpace(input.ProjectID),
		ServiceID:        strings.TrimSpace(input.ServiceID),
		Name:             strings.TrimSpace(input.Name),
		HostIP:           strings.TrimSpace(input.HostIP),
		HostPort:         input.HostPort,
		ContainerPort:    input.ContainerPort,
		Protocol:         protocol,
		ExposureScope:    exposureScope,
		Status:           status,
		DomainName:       strings.ToLower(strings.TrimSpace(input.DomainName)),
		DomainScheme:     strings.ToLower(strings.TrimSpace(input.DomainScheme)),
		DomainTLSEnabled: input.DomainTLSEnabled,
		AccessURL:        strings.TrimSpace(input.AccessURL),
		Owner:            strings.TrimSpace(input.Owner),
		ExpiresAt:        input.ExpiresAt,
		Config:           ensureMap(input.Config),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func templateFromInput(input domaindocker.TemplateInput) domaindocker.Template {
	now := time.Now().UTC()
	kind := strings.TrimSpace(input.TemplateKind)
	if kind == "" {
		kind = "compose"
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	return domaindocker.Template{
		ID:             id,
		Name:           strings.TrimSpace(input.Name),
		Description:    strings.TrimSpace(input.Description),
		TemplateKind:   kind,
		ComposeContent: input.ComposeContent,
		EnvContent:     input.EnvContent,
		Variables:      ensureMap(input.Variables),
		Enabled:        input.Enabled,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func operationFromInput(input domaindocker.OperationInput) domaindocker.Operation {
	now := time.Now().UTC()
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "queued"
	}
	maxRetries := input.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 1800
	}
	return domaindocker.Operation{
		ID:             uuid.NewString(),
		HostID:         strings.TrimSpace(input.HostID),
		ProjectID:      strings.TrimSpace(input.ProjectID),
		ServiceID:      strings.TrimSpace(input.ServiceID),
		OperationKind:  strings.TrimSpace(input.OperationKind),
		Status:         status,
		RequestedBy:    strings.TrimSpace(input.RequestedBy),
		MaxRetries:     maxRetries,
		TimeoutSeconds: timeoutSeconds,
		Payload:        ensureMap(input.Payload),
		Result:         ensureMap(input.Result),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func scanHost(rows scanner) (domaindocker.Host, error) {
	var item domaindocker.Host
	var labels, config []byte
	if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Endpoint, &item.AgentID, &item.AgentVersion,
		&item.DockerVersion, &item.ComposeVersion, &item.Architecture, &item.Environment, &item.Owner, &item.Team,
		&item.VirtualizationConnectionID, &item.VMID, &item.VMName, &item.IPAddress, &item.CPUCoreCount,
		&item.MemoryBytes, &item.DiskBytes, &item.AvailablePortStart, &item.AvailablePortEnd, &labels, &config,
		&item.LastHeartbeatAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindocker.Host{}, fmt.Errorf("scan docker host: %w", err)
	}
	unmarshalMap(labels, &item.Labels)
	unmarshalMap(config, &item.Config)
	return item, nil
}

func scanHostRow(row *sql.Row) (domaindocker.Host, error) {
	item, err := scanHost(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			return domaindocker.Host{}, ErrNotFound
		}
		return domaindocker.Host{}, err
	}
	return item, nil
}

func scanProject(rows scanner) (domaindocker.Project, error) {
	var item domaindocker.Project
	var labels, config []byte
	if err := rows.Scan(&item.ID, &item.HostID, &item.Name, &item.Slug, &item.Description, &item.Environment,
		&item.Owner, &item.Team, &item.SourceKind, &item.SourceRef, &item.ComposeContent, &item.EnvContent,
		&item.Status, &item.DesiredState, &item.TemplateID, &item.TTLSeconds, &item.ExpiresAt, &item.LastDeployedAt,
		&labels, &config, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindocker.Project{}, fmt.Errorf("scan docker project: %w", err)
	}
	unmarshalMap(labels, &item.Labels)
	unmarshalMap(config, &item.Config)
	return item, nil
}

func scanProjectRow(row *sql.Row) (domaindocker.Project, error) {
	item, err := scanProject(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			return domaindocker.Project{}, ErrNotFound
		}
		return domaindocker.Project{}, err
	}
	return item, nil
}

func scanService(rows scanner) (domaindocker.Service, error) {
	var item domaindocker.Service
	var config []byte
	if err := rows.Scan(&item.ID, &item.ProjectID, &item.HostID, &item.Name, &item.Image, &item.Status,
		&item.ContainerID, &item.RestartCount, &item.CPUPercent, &item.MemoryBytes, &item.NetworkRxBytes,
		&item.NetworkTxBytes, &config, &item.LastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindocker.Service{}, fmt.Errorf("scan docker service: %w", err)
	}
	unmarshalMap(config, &item.Config)
	return item, nil
}

func scanServiceRow(row *sql.Row) (domaindocker.Service, error) {
	item, err := scanService(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			return domaindocker.Service{}, ErrNotFound
		}
		return domaindocker.Service{}, err
	}
	return item, nil
}

func scanPortMapping(rows scanner) (domaindocker.PortMapping, error) {
	var item domaindocker.PortMapping
	var config []byte
	if err := rows.Scan(&item.ID, &item.HostID, &item.ProjectID, &item.ServiceID, &item.Name, &item.HostIP,
		&item.HostPort, &item.ContainerPort, &item.Protocol, &item.ExposureScope, &item.Status, &item.AccessURL,
		&item.DomainName, &item.DomainScheme, &item.DomainTLSEnabled, &item.Owner, &item.ExpiresAt, &config,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindocker.PortMapping{}, fmt.Errorf("scan docker port mapping: %w", err)
	}
	unmarshalMap(config, &item.Config)
	return item, nil
}

func scanPortMappingRow(row *sql.Row) (domaindocker.PortMapping, error) {
	item, err := scanPortMapping(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			return domaindocker.PortMapping{}, ErrNotFound
		}
		return domaindocker.PortMapping{}, err
	}
	return item, nil
}

func scanTemplate(rows scanner) (domaindocker.Template, error) {
	var item domaindocker.Template
	var variables []byte
	if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.TemplateKind, &item.ComposeContent,
		&item.EnvContent, &variables, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindocker.Template{}, fmt.Errorf("scan docker template: %w", err)
	}
	unmarshalMap(variables, &item.Variables)
	return item, nil
}

func scanTemplateRow(row *sql.Row) (domaindocker.Template, error) {
	item, err := scanTemplate(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			return domaindocker.Template{}, ErrNotFound
		}
		return domaindocker.Template{}, err
	}
	return item, nil
}

func scanOperation(rows scanner) (domaindocker.Operation, error) {
	var item domaindocker.Operation
	var payload, result []byte
	if err := rows.Scan(&item.ID, &item.HostID, &item.ProjectID, &item.ServiceID, &item.OperationKind, &item.Status,
		&item.RequestedBy, &item.ClaimedByWorkerID, &item.AttemptCount, &item.MaxRetries, &item.TimeoutSeconds,
		&payload, &result, &item.StartedAt, &item.LastHeartbeatAt, &item.FinishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindocker.Operation{}, fmt.Errorf("scan docker operation: %w", err)
	}
	unmarshalMap(payload, &item.Payload)
	unmarshalMap(result, &item.Result)
	return item, nil
}

func scanOperationRow(row *sql.Row) (domaindocker.Operation, error) {
	item, err := scanOperation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			return domaindocker.Operation{}, ErrNotFound
		}
		return domaindocker.Operation{}, err
	}
	return item, nil
}

func scanOperationLog(rows scanner) (domaindocker.OperationLog, error) {
	var item domaindocker.OperationLog
	var payload []byte
	if err := rows.Scan(&item.ID, &item.OperationID, &item.LogLevel, &item.Message, &payload, &item.CreatedAt); err != nil {
		return domaindocker.OperationLog{}, fmt.Errorf("scan docker operation log: %w", err)
	}
	unmarshalMap(payload, &item.Payload)
	return item, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func hostSelect() string {
	return `SELECT id, name, status, COALESCE(endpoint, ''), COALESCE(agent_id, ''), COALESCE(agent_version, ''), COALESCE(docker_version, ''), COALESCE(compose_version, ''), COALESCE(architecture, ''), COALESCE(environment, ''), COALESCE(owner, ''), COALESCE(team, ''), COALESCE(virtualization_connection_id, ''), COALESCE(vm_id, ''), COALESCE(vm_name, ''), COALESCE(ip_address, ''), cpu_core_count, memory_bytes, disk_bytes, available_port_start, available_port_end, labels, config, last_heartbeat_at, created_at, updated_at FROM docker_hosts`
}

func projectSelect() string {
	return `SELECT id, host_id, name, slug, COALESCE(description, ''), COALESCE(environment, ''), COALESCE(owner, ''), COALESCE(team, ''), COALESCE(source_kind, ''), COALESCE(source_ref, ''), COALESCE(compose_content, ''), COALESCE(env_content, ''), status, COALESCE(desired_state, ''), COALESCE(template_id, ''), ttl_seconds, expires_at, last_deployed_at, labels, config, created_at, updated_at FROM docker_projects`
}

func serviceSelect() string {
	return `SELECT id, project_id, host_id, name, COALESCE(image, ''), status, COALESCE(container_id, ''), restart_count, cpu_percent, memory_bytes, network_rx_bytes, network_tx_bytes, config, last_seen_at, created_at, updated_at FROM docker_services`
}

func portMappingSelect() string {
	return `SELECT id, host_id, COALESCE(project_id, ''), COALESCE(service_id, ''), name, COALESCE(host_ip, ''), host_port, container_port, protocol, exposure_scope, status, COALESCE(access_url, ''), COALESCE(domain_name, ''), COALESCE(domain_scheme, ''), domain_tls_enabled, COALESCE(owner, ''), expires_at, config, created_at, updated_at FROM docker_port_mappings`
}

func templateSelect() string {
	return `SELECT id, name, COALESCE(description, ''), template_kind, COALESCE(compose_content, ''), COALESCE(env_content, ''), variables, enabled, created_at, updated_at FROM docker_templates`
}

func operationSelect() string {
	return `SELECT id, COALESCE(host_id, ''), COALESCE(project_id, ''), COALESCE(service_id, ''), operation_kind, status, COALESCE(requested_by, ''), COALESCE(claimed_by_worker_id, ''), attempt_count, max_retries, timeout_seconds, payload, result, started_at, last_heartbeat_at, finished_at, created_at, updated_at FROM docker_operations`
}

func (r *Repository) getServiceByProjectName(ctx context.Context, projectID, name string) (domaindocker.Service, error) {
	row := r.db.WithContext(ctx).Raw(serviceSelect()+" WHERE project_id = ? AND name = ? LIMIT 1", strings.TrimSpace(projectID), strings.TrimSpace(name)).Row()
	return scanServiceRow(row)
}

func (r *Repository) deleteByID(ctx context.Context, tableName, id, label string) error {
	result := r.db.WithContext(ctx).Exec(fmt.Sprintf("DELETE FROM %s WHERE id = ?", tableName), strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("%s: %w", label, result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) count(ctx context.Context, tableName string, clauses []string, args []any) (int, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
	query = appendWhere(query, clauses)
	var total int
	if err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(&total); err != nil {
		return 0, fmt.Errorf("count %s: %w", tableName, err)
	}
	return total, nil
}

func hostClauses(filter domaindocker.HostFilter) ([]string, []any) {
	clauses := []string{}
	args := []any{}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Environment); value != "" {
		clauses = append(clauses, "environment = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Architecture); value != "" {
		clauses = append(clauses, "architecture = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(name) LIKE ? OR LOWER(endpoint) LIKE ? OR LOWER(agent_id) LIKE ? OR LOWER(vm_name) LIKE ? OR LOWER(ip_address) LIKE ?)")
		args = append(args, search, search, search, search, search)
	}
	return clauses, args
}

func projectClauses(filter domaindocker.ProjectFilter) ([]string, []any) {
	clauses := []string{}
	args := []any{}
	if value := strings.TrimSpace(filter.HostID); value != "" {
		clauses = append(clauses, "host_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.SourceKind); value != "" {
		if value == "compose" {
			clauses = append(clauses, "COALESCE(source_kind, '') <> ?")
			args = append(args, "single_container")
		} else {
			clauses = append(clauses, "source_kind = ?")
			args = append(args, value)
		}
	}
	if value := strings.TrimSpace(filter.Environment); value != "" {
		clauses = append(clauses, "environment = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(name) LIKE ? OR LOWER(slug) LIKE ? OR LOWER(description) LIKE ? OR LOWER(owner) LIKE ?)")
		args = append(args, search, search, search, search)
	}
	return clauses, args
}

func serviceClauses(filter domaindocker.ServiceFilter) ([]string, []any) {
	clauses := []string{}
	args := []any{}
	if value := strings.TrimSpace(filter.HostID); value != "" {
		clauses = append(clauses, "host_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ProjectID); value != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(name) LIKE ? OR LOWER(image) LIKE ? OR LOWER(container_id) LIKE ?)")
		args = append(args, search, search, search)
	}
	return clauses, args
}

func portMappingClauses(filter domaindocker.PortMappingFilter) ([]string, []any) {
	clauses := []string{}
	args := []any{}
	if value := strings.TrimSpace(filter.HostID); value != "" {
		clauses = append(clauses, "host_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ProjectID); value != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ServiceID); value != "" {
		clauses = append(clauses, "service_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if filter.HostPort > 0 {
		clauses = append(clauses, "host_port = ?")
		args = append(args, filter.HostPort)
	}
	if value := strings.TrimSpace(filter.HostIP); value != "" {
		clauses = append(clauses, "COALESCE(host_ip, '') = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Protocol); value != "" {
		clauses = append(clauses, "protocol = ?")
		args = append(args, strings.ToLower(value))
	}
	if value := strings.TrimSpace(filter.DomainName); value != "" {
		clauses = append(clauses, "LOWER(domain_name) = ?")
		args = append(args, strings.ToLower(value))
	}
	if value := strings.TrimSpace(filter.ExcludeID); value != "" {
		clauses = append(clauses, "id <> ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(name) LIKE ? OR LOWER(access_url) LIKE ? OR LOWER(domain_name) LIKE ? OR LOWER(owner) LIKE ?)")
		args = append(args, search, search, search, search)
	}
	return clauses, args
}

func templateClauses(filter domaindocker.TemplateFilter) ([]string, []any) {
	clauses := []string{}
	args := []any{}
	if filter.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, *filter.Enabled)
	}
	if value := strings.TrimSpace(filter.Kind); value != "" {
		clauses = append(clauses, "template_kind = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(name) LIKE ? OR LOWER(description) LIKE ?)")
		args = append(args, search, search)
	}
	return clauses, args
}

func operationClauses(filter domaindocker.OperationFilter) ([]string, []any) {
	clauses := []string{}
	args := []any{}
	if value := strings.TrimSpace(filter.HostID); value != "" {
		clauses = append(clauses, "host_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ProjectID); value != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ServiceID); value != "" {
		clauses = append(clauses, "service_id = ?")
		args = append(args, value)
	}
	if len(filter.Statuses) > 0 {
		statuses := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			if trimmed := strings.TrimSpace(status); trimmed != "" {
				statuses = append(statuses, trimmed)
			}
		}
		if len(statuses) > 0 {
			clauses = append(clauses, fmt.Sprintf("status IN (%s)", placeholders(len(statuses))))
			for _, status := range statuses {
				args = append(args, status)
			}
		}
	} else if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if filter.Abnormal {
		clauses = append(clauses, "status IN (?, ?)")
		args = append(args, "failed", "callback_timeout")
	}
	if filter.Pending {
		clauses = append(clauses, "status IN (?, ?)")
		args = append(args, "queued", "running")
	}
	if value := strings.TrimSpace(filter.OperationKind); value != "" {
		clauses = append(clauses, "operation_kind = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		search := "%" + strings.ToLower(value) + "%"
		clauses = append(clauses, "(LOWER(operation_kind) LIKE ? OR LOWER(status) LIKE ? OR LOWER(requested_by) LIKE ?)")
		args = append(args, search, search, search)
	}
	return clauses, args
}

func appendWhere(query string, clauses []string) string {
	if len(clauses) == 0 {
		return query
	}
	return query + " WHERE " + strings.Join(clauses, " AND ")
}

func fetchCreatedAt(ctx context.Context, db *gorm.DB, tableName, id string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(fmt.Sprintf("SELECT created_at FROM %s WHERE id = ?", tableName), strings.TrimSpace(id)).Row().Scan(&createdAt); err != nil {
		return time.Now().UTC()
	}
	return createdAt
}

func marshalJSON(value any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(value)
}

func unmarshalMap(raw []byte, target *map[string]any) {
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, target)
	}
	if *target == nil {
		*target = map[string]any{}
	}
}

func mergeJSONMap(base map[string]any, overlay map[string]any) map[string]any {
	out := ensureMap(base)
	for key, value := range overlay {
		out[key] = value
	}
	return out
}

func ensureMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func normalizedLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 50
	}
	return limit
}

func normalizedPage(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

func limitOffset(limit, page, pageSize int) (int, int) {
	size := normalizedLimit(limit)
	if pageSize > 0 {
		size = normalizedLimit(pageSize)
	}
	currentPage := normalizedPage(page)
	return size, (currentPage - 1) * size
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	items := make([]string, count)
	for i := range items {
		items[i] = "?"
	}
	return strings.Join(items, ", ")
}

func compactStrings(values []string) []string {
	items := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		items = append(items, trimmed)
	}
	return items
}

func slugify(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range normalized {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(builder.String(), "-")
	if out == "" {
		return "docker-project"
	}
	return out
}
