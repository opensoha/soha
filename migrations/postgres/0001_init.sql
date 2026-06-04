-- Consolidated PostgreSQL bootstrap migration.
-- This file contains the full current schema baseline for fresh soha databases.
-- It supersedes the previously split postgres migration files through 0025_authz_version.sql.

-- Schema only. The bootstrap account is seeded by backend startup from auth.dev_principal
-- and the repository baseline is admin / soha with no legacy migration path.

BEGIN;

-- PostgreSQL database dump



SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

-- Name: ai_access_policies; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_access_policies (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    enabled boolean DEFAULT true NOT NULL,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    ai_client_id text,
    effect text DEFAULT 'allow'::text NOT NULL,
    tool_patterns json DEFAULT '[]'::json NOT NULL,
    skill_ids json DEFAULT '[]'::json NOT NULL,
    resource_scopes json DEFAULT '{}'::json NOT NULL,
    risk_levels json DEFAULT '[]'::json NOT NULL,
    approval_policy json DEFAULT '{}'::json NOT NULL,
    conditions json DEFAULT '{}'::json NOT NULL,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: ai_agent_runs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_agent_runs (
    id text NOT NULL,
    provider_id text NOT NULL,
    provider_kind text NOT NULL,
    capability_id text NOT NULL,
    skill_ids json DEFAULT '[]'::json NOT NULL,
    session_id text,
    root_cause_run_id text,
    created_by text NOT NULL,
    status text NOT NULL,
    scope json DEFAULT '{}'::json NOT NULL,
    toolset json DEFAULT '{}'::json NOT NULL,
    tool_bindings json DEFAULT '[]'::json NOT NULL,
    skill_bindings json DEFAULT '[]'::json NOT NULL,
    input json DEFAULT '{}'::json NOT NULL,
    output json DEFAULT '{}'::json NOT NULL,
    tool_executions json DEFAULT '[]'::json NOT NULL,
    analysis_artifacts json DEFAULT '[]'::json NOT NULL,
    callback_token text NOT NULL,
    claimed_by_agent_id text,
    external_run_id text,
    error_message text,
    timeout_seconds integer DEFAULT 600 NOT NULL,
    queued_at timestamp without time zone NOT NULL,
    started_at timestamp without time zone,
    last_heartbeat_at timestamp without time zone,
    completed_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: ai_analysis_profiles; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_analysis_profiles (
    id text NOT NULL,
    name text NOT NULL,
    mode text NOT NULL,
    enabled_sources json DEFAULT '[]'::json NOT NULL,
    enabled_playbooks json DEFAULT '[]'::json NOT NULL,
    query_budgets json DEFAULT '{}'::json NOT NULL,
    output_style json DEFAULT '{}'::json NOT NULL,
    remediation_policy text DEFAULT 'suggest_only'::text NOT NULL,
    default_time_range_minutes integer DEFAULT 60 NOT NULL,
    timeout_seconds integer DEFAULT 90 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: ai_automation_policies; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_automation_policies (
    id text NOT NULL,
    name text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    trigger_type text NOT NULL,
    agent_provider_id text DEFAULT 'internal'::text NOT NULL,
    trigger_conditions json DEFAULT '{}'::json NOT NULL,
    dedup_window_seconds integer DEFAULT 900 NOT NULL,
    analysis_profile_id text NOT NULL,
    remediation_policy text DEFAULT 'suggest_only'::text NOT NULL,
    approval_policy json DEFAULT '{}'::json NOT NULL,
    cooldown_seconds integer DEFAULT 0 NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    analysis_kinds json DEFAULT '["root_cause"]'::json NOT NULL
);


-- Name: ai_clients; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_clients (
    id text NOT NULL,
    name text NOT NULL,
    kind text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    redirect_uris json DEFAULT '[]'::json NOT NULL,
    allowed_origins json DEFAULT '[]'::json NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: ai_data_sources; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_data_sources (
    id text NOT NULL,
    name text NOT NULL,
    source_type text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    credential_ref text,
    scope json DEFAULT '{}'::json NOT NULL,
    query_budget json DEFAULT '{}'::json NOT NULL,
    redaction_policy json DEFAULT '{}'::json NOT NULL,
    mcp_adapter text NOT NULL,
    config json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    source_kind text,
    backend_type text,
    validation_status text,
    validation_message text,
    last_validated_at timestamp without time zone
);


-- Name: ai_gateway_approval_requests; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_gateway_approval_requests (
    id text NOT NULL,
    status text NOT NULL,
    strategy text NOT NULL,
    policy_id text,
    approval_policy_ref text,
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    actor_name text,
    actor_roles json DEFAULT '[]'::json NOT NULL,
    actor_teams json DEFAULT '[]'::json NOT NULL,
    ai_client_id text,
    ai_client_name text,
    skill_id text,
    tool_name text NOT NULL,
    risk_level text NOT NULL,
    requires_approval boolean DEFAULT true NOT NULL,
    resource_scope json DEFAULT '{}'::json NOT NULL,
    tool_input json DEFAULT '{}'::json NOT NULL,
    related_ids json DEFAULT '{}'::json NOT NULL,
    output json DEFAULT '{}'::json NOT NULL,
    summary text NOT NULL,
    request_id text,
    source_ip text,
    decided_by text,
    decided_by_name text,
    decided_at timestamp without time zone,
    decision_comment text,
    expires_at timestamp without time zone,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: ai_gateway_audit_logs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_gateway_audit_logs (
    id text NOT NULL,
    actor_type text NOT NULL,
    actor_id text NOT NULL,
    actor_name text,
    ai_client_id text,
    ai_client_name text,
    skill_id text,
    tool_name text,
    risk_level text,
    resource_scope json DEFAULT '{}'::json NOT NULL,
    action text NOT NULL,
    result text NOT NULL,
    summary text NOT NULL,
    request_id text,
    source_ip text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone NOT NULL
);


-- Name: ai_gateway_rate_limit_counters; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_gateway_rate_limit_counters (
    key text NOT NULL,
    policy_id text NOT NULL,
    scope text NOT NULL,
    actor_type text,
    actor_id text,
    ai_client_id text,
    tool_name text,
    window_start timestamp without time zone NOT NULL,
    window_end timestamp without time zone NOT NULL,
    limit_value integer NOT NULL,
    count integer DEFAULT 0 NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: ai_gateway_rate_limit_states; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_gateway_rate_limit_states (
    key text NOT NULL,
    policy_id text NOT NULL,
    scope text NOT NULL,
    actor_type text,
    actor_id text,
    ai_client_id text,
    tool_name text,
    limit_value integer NOT NULL,
    burst_value integer DEFAULT 1 NOT NULL,
    interval_seconds double precision NOT NULL,
    tat timestamp without time zone NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: ai_gateway_skill_bindings; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_gateway_skill_bindings (
    id text NOT NULL,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    ai_client_id text,
    skill_id text NOT NULL,
    capability_refs json DEFAULT '[]'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: ai_inspection_runs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_inspection_runs (
    id text NOT NULL,
    task_id text NOT NULL,
    triggered_by text NOT NULL,
    status text NOT NULL,
    severity text NOT NULL,
    summary text NOT NULL,
    findings json DEFAULT '[]'::json NOT NULL,
    report json DEFAULT '{}'::json NOT NULL,
    started_at timestamp without time zone DEFAULT now() NOT NULL,
    completed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: ai_inspection_tasks; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_inspection_tasks (
    id text NOT NULL,
    title text NOT NULL,
    scope_type text DEFAULT 'platform'::text NOT NULL,
    cluster_id text,
    namespace text,
    checks json DEFAULT '[]'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    interval_minutes integer DEFAULT 0 NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_by text NOT NULL,
    last_run_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: ai_messages; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_messages (
    id text NOT NULL,
    session_id text NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: ai_root_cause_runs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_root_cause_runs (
    id text NOT NULL,
    title text NOT NULL,
    created_by text NOT NULL,
    status text NOT NULL,
    severity text NOT NULL,
    summary text NOT NULL,
    cluster_id text,
    namespace text,
    workload_kind text,
    workload_name text,
    alert_id text,
    time_range_minutes integer DEFAULT 60 NOT NULL,
    question text,
    evidence json DEFAULT '[]'::json NOT NULL,
    hypotheses json DEFAULT '[]'::json NOT NULL,
    recommendations json DEFAULT '[]'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    analysis_profile_id text,
    trigger_type text DEFAULT 'manual'::text NOT NULL,
    data_source_snapshot json DEFAULT '{}'::json NOT NULL,
    playbook_results json DEFAULT '{}'::json NOT NULL,
    remediation_plan json DEFAULT '{}'::json NOT NULL,
    dedup_key text,
    kind text DEFAULT 'root_cause'::text NOT NULL,
    session_id text,
    tool_executions json DEFAULT '[]'::json NOT NULL
);


-- Name: ai_sessions; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.ai_sessions (
    id text NOT NULL,
    title text NOT NULL,
    created_by text NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    deleted_at timestamp without time zone
);


-- Name: alert_delivery_logs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.alert_delivery_logs (
    id text NOT NULL,
    alert_id text NOT NULL,
    channel_id text,
    status text NOT NULL,
    summary text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: alert_events; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.alert_events (
    id text NOT NULL,
    rule_id text,
    source_type text NOT NULL,
    source_system text,
    fingerprint text NOT NULL,
    title text NOT NULL,
    summary text NOT NULL,
    severity text NOT NULL,
    status text NOT NULL,
    cluster_id text,
    namespace text,
    labels json DEFAULT '{}'::json NOT NULL,
    annotations json DEFAULT '{}'::json NOT NULL,
    receiver text,
    generator_url text,
    current_state text,
    last_notification_at timestamp without time zone,
    starts_at timestamp without time zone,
    ends_at timestamp without time zone,
    last_seen_at timestamp without time zone DEFAULT now() NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: alert_integrations; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.alert_integrations (
    id text NOT NULL,
    name text NOT NULL,
    integration_type text NOT NULL,
    description text,
    token text NOT NULL,
    label_mapping json DEFAULT '{}'::json NOT NULL,
    dedupe_config json DEFAULT '{}'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    last_error text,
    last_received_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: alert_rule_runs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.alert_rule_runs (
    id text NOT NULL,
    rule_id text NOT NULL,
    status text NOT NULL,
    summary text,
    result json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    matched boolean DEFAULT false NOT NULL,
    duration_ms integer DEFAULT 0 NOT NULL,
    error text
);


-- Name: alert_rules; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.alert_rules (
    id text NOT NULL,
    name text NOT NULL,
    rule_type text NOT NULL,
    datasource_selector json DEFAULT '{}'::json NOT NULL,
    query_spec json DEFAULT '{}'::json NOT NULL,
    threshold_spec json DEFAULT '{}'::json NOT NULL,
    for_seconds integer DEFAULT 0 NOT NULL,
    group_by json DEFAULT '[]'::json NOT NULL,
    labels json DEFAULT '{}'::json NOT NULL,
    annotations json DEFAULT '{}'::json NOT NULL,
    notification_policy_id text,
    healing_policy_ids json DEFAULT '[]'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: alert_silences; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.alert_silences (
    id text NOT NULL,
    name text NOT NULL,
    matchers json DEFAULT '{}'::json NOT NULL,
    reason text,
    starts_at timestamp without time zone NOT NULL,
    ends_at timestamp without time zone NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: announcement_receipts; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.announcement_receipts (
    id text NOT NULL,
    announcement_id text NOT NULL,
    user_id text NOT NULL,
    read_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: announcements; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.announcements (
    id text NOT NULL,
    title text NOT NULL,
    summary text,
    content text NOT NULL,
    level text DEFAULT 'info'::text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    audience text DEFAULT 'all'::text NOT NULL,
    sticky boolean DEFAULT false NOT NULL,
    starts_at timestamp without time zone,
    ends_at timestamp without time zone,
    published_at timestamp without time zone,
    created_by text,
    updated_by text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: app_settings; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.app_settings (
    setting_key text NOT NULL,
    category text NOT NULL,
    value json DEFAULT '{}'::json NOT NULL,
    updated_by text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: application_build_sources; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.application_build_sources (
    id text NOT NULL,
    application_id text NOT NULL,
    source_name text NOT NULL,
    source_type text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    build_image text,
    default_tag text,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: application_environments; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.application_environments (
    id text NOT NULL,
    application_id text NOT NULL,
    environment_id text NOT NULL,
    workflow_template_id text,
    build_policy json DEFAULT '{}'::json NOT NULL,
    release_policy json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    strategy_profile_id text,
    promotion_policy_id text,
    approval_policy_id text,
    artifact_policy_id text,
    resource_selector jsonb DEFAULT '{}'::jsonb NOT NULL
);


-- Name: application_service_containers; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.application_service_containers (
    id text NOT NULL,
    service_id text NOT NULL,
    container_name text NOT NULL,
    image_repository text,
    default_tag_template text,
    dockerfile_path text,
    build_context_dir text,
    runtime_ports jsonb DEFAULT '[]'::jsonb NOT NULL,
    env_schema jsonb DEFAULT '{}'::jsonb NOT NULL,
    resource_profile jsonb DEFAULT '{}'::jsonb NOT NULL,
    health_check jsonb DEFAULT '{}'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: application_services; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.application_services (
    id text NOT NULL,
    application_id text NOT NULL,
    service_key text NOT NULL,
    service_name text NOT NULL,
    description text,
    service_kind text DEFAULT 'kubernetes_workload'::text NOT NULL,
    owner_team text,
    repository_provider text,
    repository_project_id text,
    repository_path text,
    default_branch text,
    build_source_id text,
    enabled boolean DEFAULT true NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: applications; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.applications (
    id text NOT NULL,
    name text NOT NULL,
    app_key text NOT NULL,
    app_group text NOT NULL,
    language text NOT NULL,
    description text,
    owner_team text,
    repository_provider text,
    repository_project_id text,
    repository_path text,
    default_branch text,
    default_tag text,
    build_image text,
    build_context_dir text,
    dockerfile_path text,
    enabled boolean DEFAULT true NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    business_line_id text
);


-- Name: approval_policies; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.approval_policies (
    id text NOT NULL,
    policy_key text NOT NULL,
    name text NOT NULL,
    description text,
    mode text DEFAULT 'single'::text NOT NULL,
    required_approvals integer DEFAULT 1 NOT NULL,
    sla_minutes integer DEFAULT 60 NOT NULL,
    approver_roles jsonb DEFAULT '[]'::jsonb NOT NULL,
    change_window jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: audit_logs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.audit_logs (
    id text NOT NULL,
    actor_id text NOT NULL,
    actor_name text,
    roles json DEFAULT '[]'::json NOT NULL,
    teams json DEFAULT '[]'::json NOT NULL,
    cluster_id text,
    namespace text,
    resource_kind text,
    resource_name text,
    action text NOT NULL,
    result text NOT NULL,
    summary text NOT NULL,
    request_path text,
    request_method text,
    request_id text,
    source_ip text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: auth_ephemeral_tokens; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.auth_ephemeral_tokens (
    token text NOT NULL,
    kind text NOT NULL,
    payload json DEFAULT '{}'::json NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: build_records; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.build_records (
    id text NOT NULL,
    project_id text,
    source_system text,
    status text NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    started_at timestamp without time zone,
    finished_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: build_templates; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.build_templates (
    id text NOT NULL,
    template_key text NOT NULL,
    name text NOT NULL,
    description text,
    builder_kind text DEFAULT 'custom'::text NOT NULL,
    dockerfile_template text,
    build_commands jsonb DEFAULT '[]'::jsonb NOT NULL,
    variable_schema jsonb DEFAULT '{}'::jsonb NOT NULL,
    default_variables jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: cluster_credentials_meta; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.cluster_credentials_meta (
    id text NOT NULL,
    cluster_id text NOT NULL,
    credential_type text NOT NULL,
    source_type text NOT NULL,
    source_ref text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: clusters; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.clusters (
    id text NOT NULL,
    name text NOT NULL,
    region text,
    environment text,
    labels json DEFAULT '{}'::json NOT NULL,
    connection_mode text DEFAULT 'direct_kubeconfig'::text NOT NULL,
    capabilities json DEFAULT '[]'::json NOT NULL,
    health_snapshot json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    version text
);


-- Name: delivery_blueprints; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.delivery_blueprints (
    id text NOT NULL,
    blueprint_key text NOT NULL,
    name text NOT NULL,
    description text,
    application_draft jsonb DEFAULT '{}'::jsonb NOT NULL,
    build_sources jsonb DEFAULT '[]'::jsonb NOT NULL,
    environment_bindings jsonb DEFAULT '[]'::jsonb NOT NULL,
    file_templates jsonb DEFAULT '[]'::jsonb NOT NULL,
    execution_hints jsonb DEFAULT '{}'::jsonb NOT NULL,
    post_create_actions jsonb DEFAULT '[]'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: delivery_environments; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.delivery_environments (
    id text NOT NULL,
    environment_key text NOT NULL,
    name text NOT NULL,
    tier text,
    stage_level integer DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    is_production boolean DEFAULT false NOT NULL,
    requires_approval boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: deploy_records; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.deploy_records (
    id text NOT NULL,
    project_id text,
    cluster_id text,
    namespace text,
    release_name text,
    status text NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    deployed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: docker_hosts; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.docker_hosts (
    id text NOT NULL,
    name text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    endpoint text,
    agent_id text,
    agent_version text,
    docker_version text,
    compose_version text,
    environment text,
    owner text,
    team text,
    virtualization_connection_id text,
    vm_id text,
    vm_name text,
    ip_address text,
    cpu_core_count integer DEFAULT 0 NOT NULL,
    memory_bytes bigint DEFAULT 0 NOT NULL,
    disk_bytes bigint DEFAULT 0 NOT NULL,
    available_port_start integer DEFAULT 20000 NOT NULL,
    available_port_end integer DEFAULT 39999 NOT NULL,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_heartbeat_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    architecture text
);


-- Name: docker_operation_logs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.docker_operation_logs (
    id text NOT NULL,
    operation_id text NOT NULL,
    log_level text NOT NULL,
    message text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: docker_operations; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.docker_operations (
    id text NOT NULL,
    host_id text,
    project_id text,
    service_id text,
    operation_kind text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    requested_by text,
    claimed_by_worker_id text,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_retries integer DEFAULT 1 NOT NULL,
    timeout_seconds integer DEFAULT 1800 NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    result jsonb DEFAULT '{}'::jsonb NOT NULL,
    started_at timestamp with time zone,
    last_heartbeat_at timestamp with time zone,
    finished_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: docker_port_mappings; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.docker_port_mappings (
    id text NOT NULL,
    host_id text NOT NULL,
    project_id text,
    service_id text,
    name text NOT NULL,
    host_ip text,
    host_port integer NOT NULL,
    container_port integer NOT NULL,
    protocol text DEFAULT 'tcp'::text NOT NULL,
    exposure_scope text DEFAULT 'internal'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    domain_name text,
    domain_scheme text,
    domain_tls_enabled boolean DEFAULT false NOT NULL,
    access_url text,
    owner text,
    expires_at timestamp with time zone,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: docker_projects; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.docker_projects (
    id text NOT NULL,
    host_id text NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    description text,
    environment text,
    owner text,
    team text,
    source_kind text,
    source_ref text,
    compose_content text,
    env_content text,
    status text DEFAULT 'draft'::text NOT NULL,
    desired_state text,
    template_id text,
    ttl_seconds integer DEFAULT 0 NOT NULL,
    expires_at timestamp with time zone,
    last_deployed_at timestamp with time zone,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: docker_services; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.docker_services (
    id text NOT NULL,
    project_id text NOT NULL,
    host_id text NOT NULL,
    name text NOT NULL,
    image text,
    status text DEFAULT 'unknown'::text NOT NULL,
    container_id text,
    restart_count integer DEFAULT 0 NOT NULL,
    cpu_percent double precision DEFAULT 0 NOT NULL,
    memory_bytes bigint DEFAULT 0 NOT NULL,
    network_rx_bytes bigint DEFAULT 0 NOT NULL,
    network_tx_bytes bigint DEFAULT 0 NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: docker_templates; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.docker_templates (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    template_kind text DEFAULT 'compose'::text NOT NULL,
    compose_content text,
    env_content text,
    variables jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: event_stream; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.event_stream (
    id text NOT NULL,
    source text NOT NULL,
    category text NOT NULL,
    severity text NOT NULL,
    cluster_id text,
    namespace text,
    resource_ref json DEFAULT '{}'::json NOT NULL,
    summary text NOT NULL,
    payload json DEFAULT '{}'::json NOT NULL,
    correlation_id text,
    occurred_at timestamp without time zone DEFAULT now() NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: execution_artifacts; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.execution_artifacts (
    id text NOT NULL,
    execution_task_id text NOT NULL,
    release_bundle_id text,
    application_id text NOT NULL,
    application_environment_id text,
    artifact_kind text NOT NULL,
    name text,
    ref text,
    digest text,
    path text,
    status text,
    size_bytes bigint DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: execution_callbacks; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.execution_callbacks (
    id text NOT NULL,
    execution_task_id text NOT NULL,
    provider_kind text NOT NULL,
    status text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: execution_logs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.execution_logs (
    id text NOT NULL,
    execution_task_id text NOT NULL,
    log_level text NOT NULL,
    message text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: execution_tasks; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.execution_tasks (
    id text NOT NULL,
    release_bundle_id text,
    application_id text NOT NULL,
    application_environment_id text,
    task_kind text NOT NULL,
    provider_kind text NOT NULL,
    target_kind text DEFAULT 'k8s_workload'::text NOT NULL,
    status text NOT NULL,
    queue_key text,
    lock_key text,
    max_retries integer DEFAULT 0 NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    timeout_seconds integer DEFAULT 300 NOT NULL,
    callback_token text,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    result jsonb DEFAULT '{}'::jsonb NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    last_heartbeat_at timestamp with time zone,
    claimed_by_agent_id text,
    runtime_endpoint text,
    runtime_cluster_id text,
    stop_transport text,
    last_runtime_seen_at timestamp with time zone
);


-- Name: healing_policies; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.healing_policies (
    id text NOT NULL,
    name text NOT NULL,
    trigger_mode text NOT NULL,
    workflow_template_id text NOT NULL,
    approval_policy_ref text,
    cooldown_seconds integer DEFAULT 0 NOT NULL,
    concurrency_key text,
    safety_window_seconds integer DEFAULT 0 NOT NULL,
    definition json DEFAULT '{}'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: healing_runs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.healing_runs (
    id text NOT NULL,
    policy_id text NOT NULL,
    event_id text,
    status text NOT NULL,
    approval_status text,
    approval_comment text,
    requested_by text,
    approved_by text,
    workflow_run_id text,
    result json DEFAULT '{}'::json NOT NULL,
    started_at timestamp without time zone,
    completed_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: mcp_tool_grants; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.mcp_tool_grants (
    id text NOT NULL,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    ai_client_id text,
    tool_name text NOT NULL,
    effect text DEFAULT 'allow'::text NOT NULL,
    risk_level text DEFAULT 'read'::text NOT NULL,
    permission_keys json DEFAULT '[]'::json NOT NULL,
    resource_scopes json DEFAULT '{}'::json NOT NULL,
    requires_approval boolean DEFAULT false NOT NULL,
    expires_at timestamp without time zone,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: menu_role_bindings; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.menu_role_bindings (
    id text NOT NULL,
    menu_id text NOT NULL,
    role_id text NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: menus; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.menus (
    id text NOT NULL,
    parent_id text,
    path text NOT NULL,
    label_zh text NOT NULL,
    label_en text NOT NULL,
    icon_key text NOT NULL,
    section text,
    sort_order integer DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: notification_channels; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.notification_channels (
    id text NOT NULL,
    name text NOT NULL,
    channel_type text NOT NULL,
    config json DEFAULT '{}'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: notification_policies; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.notification_policies (
    id text NOT NULL,
    name text NOT NULL,
    matchers json DEFAULT '{}'::json NOT NULL,
    processor_chain json DEFAULT '[]'::json NOT NULL,
    channel_refs json DEFAULT '[]'::json NOT NULL,
    oncall_ref text,
    send_resolved boolean DEFAULT false NOT NULL,
    cooldown_seconds integer DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: notification_templates; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.notification_templates (
    id text NOT NULL,
    name text NOT NULL,
    template_type text NOT NULL,
    content_type text NOT NULL,
    body_template text,
    headers json DEFAULT '{}'::json NOT NULL,
    query_params json DEFAULT '{}'::json NOT NULL,
    sample_payload json DEFAULT '{}'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: oncall_assignment_rules; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.oncall_assignment_rules (
    id text NOT NULL,
    name text NOT NULL,
    integration_id text,
    integration_type text,
    business_line_id text,
    alert_category text,
    alert_name text,
    severity text,
    service text,
    role text,
    matchers json DEFAULT '{}'::json NOT NULL,
    target_type text NOT NULL,
    target_ref text NOT NULL,
    route_order integer DEFAULT 100 NOT NULL,
    group_by json DEFAULT '[]'::json NOT NULL,
    priority integer DEFAULT 100 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: oncall_escalation_policies; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.oncall_escalation_policies (
    id text NOT NULL,
    name text NOT NULL,
    steps json DEFAULT '[]'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: oncall_rotations; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.oncall_rotations (
    id text NOT NULL,
    schedule_id text NOT NULL,
    name text NOT NULL,
    participants json DEFAULT '[]'::json NOT NULL,
    rotation_config json DEFAULT '{}'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: oncall_schedules; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.oncall_schedules (
    id text NOT NULL,
    name text NOT NULL,
    time_zone text,
    description text,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: operation_logs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.operation_logs (
    id text NOT NULL,
    actor_id text NOT NULL,
    actor_name text,
    operation_type text NOT NULL,
    target_scope json DEFAULT '{}'::json NOT NULL,
    result text NOT NULL,
    summary text,
    request_path text,
    request_method text,
    request_id text,
    source_ip text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: personal_access_tokens; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.personal_access_tokens (
    id text NOT NULL,
    user_id text NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    scopes json DEFAULT '[]'::json NOT NULL,
    permission_keys json DEFAULT '[]'::json NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    expires_at timestamp without time zone,
    last_used_at timestamp without time zone,
    revoked_at timestamp without time zone,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: policies; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.policies (
    id text NOT NULL,
    name text NOT NULL,
    effect text NOT NULL,
    priority integer DEFAULT 0 NOT NULL,
    subjects json DEFAULT '{}'::json NOT NULL,
    targets json DEFAULT '{}'::json NOT NULL,
    actions json DEFAULT '[]'::json NOT NULL,
    conditions json DEFAULT '{}'::json NOT NULL,
    reason text,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: port_forward_sessions; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.port_forward_sessions (
    session_id text NOT NULL,
    cluster_id text NOT NULL,
    namespace text NOT NULL,
    target_kind text NOT NULL,
    target_name text NOT NULL,
    local_port integer NOT NULL,
    remote_port integer NOT NULL,
    status text NOT NULL,
    connection_mode text DEFAULT 'direct'::text NOT NULL,
    last_error text,
    created_by text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: projects; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.projects (
    id text NOT NULL,
    team_id text,
    name text NOT NULL,
    slug text NOT NULL,
    environment text,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: registry_connections; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.registry_connections (
    id text NOT NULL,
    name text NOT NULL,
    registry_type text NOT NULL,
    endpoint text NOT NULL,
    namespace text,
    username text,
    secret text,
    insecure boolean DEFAULT false NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: release_bundles; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.release_bundles (
    id text NOT NULL,
    application_id text NOT NULL,
    application_environment_id text,
    version text NOT NULL,
    source_type text NOT NULL,
    status text NOT NULL,
    artifact_ref text,
    artifact_digest text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: release_targets; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.release_targets (
    id text NOT NULL,
    application_environment_id text NOT NULL,
    cluster_id text NOT NULL,
    namespace text NOT NULL,
    workload_kind text NOT NULL,
    workload_name text NOT NULL,
    container_name text,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    target_kind text DEFAULT 'k8s_workload'::text NOT NULL,
    executor_kind text DEFAULT 'k8s_job_runner'::text NOT NULL,
    group_key text,
    wave_key text,
    region_key text,
    config_ref text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);


-- Name: roles; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.roles (
    id text NOT NULL,
    name text NOT NULL,
    scope text DEFAULT 'system'::text NOT NULL,
    capabilities json DEFAULT '[]'::json NOT NULL,
    permission_keys json DEFAULT '[]'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: scope_grants; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.scope_grants (
    id text NOT NULL,
    subject_type text NOT NULL,
    subject_id text NOT NULL,
    business_line_id text NOT NULL,
    environment_ids json DEFAULT '[]'::json NOT NULL,
    application_ids json DEFAULT '[]'::json NOT NULL,
    role text NOT NULL,
    effect text DEFAULT 'allow'::text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: service_account_tokens; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.service_account_tokens (
    id text NOT NULL,
    service_account_id text NOT NULL,
    name text NOT NULL,
    token_hash text NOT NULL,
    token_prefix text NOT NULL,
    scopes json DEFAULT '[]'::json NOT NULL,
    permission_keys json DEFAULT '[]'::json NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    expires_at timestamp without time zone,
    last_used_at timestamp without time zone,
    revoked_at timestamp without time zone,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: service_accounts; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.service_accounts (
    id text NOT NULL,
    name text NOT NULL,
    description text,
    status text DEFAULT 'active'::text NOT NULL,
    owner_user_id text,
    role_ids json DEFAULT '[]'::json NOT NULL,
    team_ids json DEFAULT '[]'::json NOT NULL,
    scope_grant_ids json DEFAULT '[]'::json NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_by text NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL
);


-- Name: sessions; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.sessions (
    id text NOT NULL,
    user_id text NOT NULL,
    refresh_token_id text NOT NULL,
    provider_type text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    last_seen_at timestamp without time zone,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    authz_version bigint DEFAULT 1 NOT NULL
);


-- Name: teams; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.teams (
    id text NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    parent_id text,
    org_path text,
    source text DEFAULT 'local'::text NOT NULL,
    external_id text
);


-- Name: user_identities; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.user_identities (
    id text NOT NULL,
    user_id text NOT NULL,
    provider_type text NOT NULL,
    provider_id text NOT NULL,
    provider_user_id text NOT NULL,
    profile json DEFAULT '{}'::json NOT NULL,
    last_login_at timestamp without time zone,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: user_password_credentials; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.user_password_credentials (
    user_id text NOT NULL,
    password_hash text NOT NULL,
    password_updated_at timestamp without time zone DEFAULT now() NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: user_project_bindings; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.user_project_bindings (
    id text NOT NULL,
    user_id text NOT NULL,
    project_id text NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: user_role_bindings; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.user_role_bindings (
    id text NOT NULL,
    user_id text NOT NULL,
    role_id text NOT NULL,
    scope json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    source text DEFAULT 'local'::text NOT NULL,
    provider_id text
);


-- Name: user_team_bindings; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.user_team_bindings (
    id text NOT NULL,
    user_id text NOT NULL,
    team_id text NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    source text DEFAULT 'local'::text NOT NULL,
    provider_id text
);


-- Name: users; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.users (
    id text NOT NULL,
    username text NOT NULL,
    email text NOT NULL,
    display_name text,
    status text DEFAULT 'active'::text NOT NULL,
    tags json DEFAULT '[]'::json NOT NULL,
    preferences json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    authz_version bigint DEFAULT 1 NOT NULL
);


-- Name: virtualization_connections; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.virtualization_connections (
    id text NOT NULL,
    provider text NOT NULL,
    name text NOT NULL,
    endpoint text,
    kubernetes_cluster_id text,
    default_namespace text,
    enabled boolean DEFAULT true NOT NULL,
    verify_tls boolean DEFAULT true NOT NULL,
    encrypted_credential jsonb DEFAULT '{}'::jsonb NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    health jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_synced_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: virtualization_flavors; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.virtualization_flavors (
    id text NOT NULL,
    provider text NOT NULL,
    connection_id text,
    external_id text NOT NULL,
    name text NOT NULL,
    status text NOT NULL,
    cpu_cores integer DEFAULT 0 NOT NULL,
    memory_mb integer DEFAULT 0 NOT NULL,
    disk_gb integer DEFAULT 0 NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    raw jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: virtualization_images; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.virtualization_images (
    id text NOT NULL,
    provider text NOT NULL,
    connection_id text NOT NULL,
    external_id text NOT NULL,
    name text NOT NULL,
    status text NOT NULL,
    os_type text,
    architecture text,
    size_bytes bigint DEFAULT 0 NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    raw jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: virtualization_task_logs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.virtualization_task_logs (
    id text NOT NULL,
    task_id text NOT NULL,
    log_level text NOT NULL,
    message text NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: virtualization_tasks; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.virtualization_tasks (
    id text NOT NULL,
    provider text NOT NULL,
    connection_id text,
    vm_id text,
    task_kind text NOT NULL,
    status text NOT NULL,
    requested_by text,
    claimed_by_worker_id text,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_retries integer DEFAULT 1 NOT NULL,
    timeout_seconds integer DEFAULT 1800 NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    result jsonb DEFAULT '{}'::jsonb NOT NULL,
    started_at timestamp with time zone,
    last_heartbeat_at timestamp with time zone,
    finished_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: virtualization_vms; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.virtualization_vms (
    id text NOT NULL,
    provider text NOT NULL,
    connection_id text NOT NULL,
    external_id text NOT NULL,
    name text NOT NULL,
    namespace text,
    status text NOT NULL,
    power_state text,
    node_name text,
    image_id text,
    flavor_id text,
    ip_addresses jsonb DEFAULT '[]'::jsonb NOT NULL,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    raw jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: workflow_approvals; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.workflow_approvals (
    id text NOT NULL,
    workflow_run_id text NOT NULL,
    node_id text NOT NULL,
    action text NOT NULL,
    comment text,
    actor_id text NOT NULL,
    actor_name text,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


-- Name: workflow_runs; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.workflow_runs (
    id text NOT NULL,
    application_id text NOT NULL,
    workflow_name text NOT NULL,
    cluster_id text,
    namespace text,
    deployment_name text,
    status text NOT NULL,
    steps json DEFAULT '[]'::json NOT NULL,
    metadata json DEFAULT '{}'::json NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: workflow_templates; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.workflow_templates (
    id text NOT NULL,
    template_key text NOT NULL,
    name text NOT NULL,
    description text,
    category text,
    definition json DEFAULT '{}'::json NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);


-- Name: ai_access_policies ai_access_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_access_policies
    ADD CONSTRAINT ai_access_policies_pkey PRIMARY KEY (id);


-- Name: ai_agent_runs ai_agent_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_agent_runs
    ADD CONSTRAINT ai_agent_runs_pkey PRIMARY KEY (id);


-- Name: ai_analysis_profiles ai_analysis_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_analysis_profiles
    ADD CONSTRAINT ai_analysis_profiles_pkey PRIMARY KEY (id);


-- Name: ai_automation_policies ai_automation_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_automation_policies
    ADD CONSTRAINT ai_automation_policies_pkey PRIMARY KEY (id);


-- Name: ai_clients ai_clients_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_clients
    ADD CONSTRAINT ai_clients_pkey PRIMARY KEY (id);


-- Name: ai_data_sources ai_data_sources_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_data_sources
    ADD CONSTRAINT ai_data_sources_pkey PRIMARY KEY (id);


-- Name: ai_gateway_approval_requests ai_gateway_approval_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_gateway_approval_requests
    ADD CONSTRAINT ai_gateway_approval_requests_pkey PRIMARY KEY (id);


-- Name: ai_gateway_audit_logs ai_gateway_audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_gateway_audit_logs
    ADD CONSTRAINT ai_gateway_audit_logs_pkey PRIMARY KEY (id);


-- Name: ai_gateway_rate_limit_counters ai_gateway_rate_limit_counters_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_gateway_rate_limit_counters
    ADD CONSTRAINT ai_gateway_rate_limit_counters_pkey PRIMARY KEY (key);


-- Name: ai_gateway_rate_limit_states ai_gateway_rate_limit_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_gateway_rate_limit_states
    ADD CONSTRAINT ai_gateway_rate_limit_states_pkey PRIMARY KEY (key);


-- Name: ai_gateway_skill_bindings ai_gateway_skill_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_gateway_skill_bindings
    ADD CONSTRAINT ai_gateway_skill_bindings_pkey PRIMARY KEY (id);


-- Name: ai_inspection_runs ai_inspection_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_inspection_runs
    ADD CONSTRAINT ai_inspection_runs_pkey PRIMARY KEY (id);


-- Name: ai_inspection_tasks ai_inspection_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_inspection_tasks
    ADD CONSTRAINT ai_inspection_tasks_pkey PRIMARY KEY (id);


-- Name: ai_messages ai_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_messages
    ADD CONSTRAINT ai_messages_pkey PRIMARY KEY (id);


-- Name: ai_root_cause_runs ai_root_cause_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_root_cause_runs
    ADD CONSTRAINT ai_root_cause_runs_pkey PRIMARY KEY (id);


-- Name: ai_sessions ai_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_sessions
    ADD CONSTRAINT ai_sessions_pkey PRIMARY KEY (id);


-- Name: alert_delivery_logs alert_delivery_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.alert_delivery_logs
    ADD CONSTRAINT alert_delivery_logs_pkey PRIMARY KEY (id);


-- Name: alert_events alert_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.alert_events
    ADD CONSTRAINT alert_events_pkey PRIMARY KEY (id);


-- Name: alert_integrations alert_integrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.alert_integrations
    ADD CONSTRAINT alert_integrations_pkey PRIMARY KEY (id);


-- Name: alert_rule_runs alert_rule_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.alert_rule_runs
    ADD CONSTRAINT alert_rule_runs_pkey PRIMARY KEY (id);


-- Name: alert_rules alert_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.alert_rules
    ADD CONSTRAINT alert_rules_pkey PRIMARY KEY (id);


-- Name: alert_silences alert_silences_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.alert_silences
    ADD CONSTRAINT alert_silences_pkey PRIMARY KEY (id);


-- Name: announcement_receipts announcement_receipts_announcement_id_user_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.announcement_receipts
    ADD CONSTRAINT announcement_receipts_announcement_id_user_id_key UNIQUE (announcement_id, user_id);


-- Name: announcement_receipts announcement_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.announcement_receipts
    ADD CONSTRAINT announcement_receipts_pkey PRIMARY KEY (id);


-- Name: announcements announcements_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.announcements
    ADD CONSTRAINT announcements_pkey PRIMARY KEY (id);


-- Name: app_settings app_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.app_settings
    ADD CONSTRAINT app_settings_pkey PRIMARY KEY (setting_key);


-- Name: application_build_sources application_build_sources_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_build_sources
    ADD CONSTRAINT application_build_sources_pkey PRIMARY KEY (id);


-- Name: application_environments application_environments_application_id_environment_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_environments
    ADD CONSTRAINT application_environments_application_id_environment_id_key UNIQUE (application_id, environment_id);


-- Name: application_environments application_environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_environments
    ADD CONSTRAINT application_environments_pkey PRIMARY KEY (id);


-- Name: application_service_containers application_service_containers_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_service_containers
    ADD CONSTRAINT application_service_containers_pkey PRIMARY KEY (id);


-- Name: application_service_containers application_service_containers_service_name_unique; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_service_containers
    ADD CONSTRAINT application_service_containers_service_name_unique UNIQUE (service_id, container_name);


-- Name: application_services application_services_application_key_unique; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_services
    ADD CONSTRAINT application_services_application_key_unique UNIQUE (application_id, service_key);


-- Name: application_services application_services_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_services
    ADD CONSTRAINT application_services_pkey PRIMARY KEY (id);


-- Name: applications applications_app_key_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.applications
    ADD CONSTRAINT applications_app_key_key UNIQUE (app_key);


-- Name: applications applications_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.applications
    ADD CONSTRAINT applications_pkey PRIMARY KEY (id);


-- Name: approval_policies approval_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.approval_policies
    ADD CONSTRAINT approval_policies_pkey PRIMARY KEY (id);


-- Name: approval_policies approval_policies_policy_key_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.approval_policies
    ADD CONSTRAINT approval_policies_policy_key_key UNIQUE (policy_key);


-- Name: audit_logs audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);


-- Name: auth_ephemeral_tokens auth_ephemeral_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.auth_ephemeral_tokens
    ADD CONSTRAINT auth_ephemeral_tokens_pkey PRIMARY KEY (token);


-- Name: build_records build_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.build_records
    ADD CONSTRAINT build_records_pkey PRIMARY KEY (id);


-- Name: build_templates build_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.build_templates
    ADD CONSTRAINT build_templates_pkey PRIMARY KEY (id);


-- Name: build_templates build_templates_template_key_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.build_templates
    ADD CONSTRAINT build_templates_template_key_key UNIQUE (template_key);


-- Name: cluster_credentials_meta cluster_credentials_meta_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.cluster_credentials_meta
    ADD CONSTRAINT cluster_credentials_meta_pkey PRIMARY KEY (id);


-- Name: clusters clusters_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.clusters
    ADD CONSTRAINT clusters_pkey PRIMARY KEY (id);


-- Name: delivery_blueprints delivery_blueprints_blueprint_key_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.delivery_blueprints
    ADD CONSTRAINT delivery_blueprints_blueprint_key_key UNIQUE (blueprint_key);


-- Name: delivery_blueprints delivery_blueprints_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.delivery_blueprints
    ADD CONSTRAINT delivery_blueprints_pkey PRIMARY KEY (id);


-- Name: delivery_environments delivery_environments_environment_key_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.delivery_environments
    ADD CONSTRAINT delivery_environments_environment_key_key UNIQUE (environment_key);


-- Name: delivery_environments delivery_environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.delivery_environments
    ADD CONSTRAINT delivery_environments_pkey PRIMARY KEY (id);


-- Name: deploy_records deploy_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.deploy_records
    ADD CONSTRAINT deploy_records_pkey PRIMARY KEY (id);


-- Name: docker_hosts docker_hosts_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_hosts
    ADD CONSTRAINT docker_hosts_pkey PRIMARY KEY (id);


-- Name: docker_operation_logs docker_operation_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_operation_logs
    ADD CONSTRAINT docker_operation_logs_pkey PRIMARY KEY (id);


-- Name: docker_operations docker_operations_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_operations
    ADD CONSTRAINT docker_operations_pkey PRIMARY KEY (id);


-- Name: docker_port_mappings docker_port_mappings_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_port_mappings
    ADD CONSTRAINT docker_port_mappings_pkey PRIMARY KEY (id);


-- Name: docker_projects docker_projects_host_id_slug_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_projects
    ADD CONSTRAINT docker_projects_host_id_slug_key UNIQUE (host_id, slug);


-- Name: docker_projects docker_projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_projects
    ADD CONSTRAINT docker_projects_pkey PRIMARY KEY (id);


-- Name: docker_services docker_services_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_services
    ADD CONSTRAINT docker_services_pkey PRIMARY KEY (id);


-- Name: docker_services docker_services_project_id_name_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_services
    ADD CONSTRAINT docker_services_project_id_name_key UNIQUE (project_id, name);


-- Name: docker_templates docker_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_templates
    ADD CONSTRAINT docker_templates_pkey PRIMARY KEY (id);


-- Name: event_stream event_stream_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.event_stream
    ADD CONSTRAINT event_stream_pkey PRIMARY KEY (id);


-- Name: execution_artifacts execution_artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_artifacts
    ADD CONSTRAINT execution_artifacts_pkey PRIMARY KEY (id);


-- Name: execution_callbacks execution_callbacks_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_callbacks
    ADD CONSTRAINT execution_callbacks_pkey PRIMARY KEY (id);


-- Name: execution_logs execution_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_logs
    ADD CONSTRAINT execution_logs_pkey PRIMARY KEY (id);


-- Name: execution_tasks execution_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_tasks
    ADD CONSTRAINT execution_tasks_pkey PRIMARY KEY (id);


-- Name: healing_policies healing_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.healing_policies
    ADD CONSTRAINT healing_policies_pkey PRIMARY KEY (id);


-- Name: healing_runs healing_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.healing_runs
    ADD CONSTRAINT healing_runs_pkey PRIMARY KEY (id);


-- Name: mcp_tool_grants mcp_tool_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.mcp_tool_grants
    ADD CONSTRAINT mcp_tool_grants_pkey PRIMARY KEY (id);


-- Name: menu_role_bindings menu_role_bindings_menu_id_role_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.menu_role_bindings
    ADD CONSTRAINT menu_role_bindings_menu_id_role_id_key UNIQUE (menu_id, role_id);


-- Name: menu_role_bindings menu_role_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.menu_role_bindings
    ADD CONSTRAINT menu_role_bindings_pkey PRIMARY KEY (id);


-- Name: menus menus_path_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.menus
    ADD CONSTRAINT menus_path_key UNIQUE (path);


-- Name: menus menus_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.menus
    ADD CONSTRAINT menus_pkey PRIMARY KEY (id);


-- Name: notification_channels notification_channels_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.notification_channels
    ADD CONSTRAINT notification_channels_pkey PRIMARY KEY (id);


-- Name: notification_policies notification_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.notification_policies
    ADD CONSTRAINT notification_policies_pkey PRIMARY KEY (id);


-- Name: notification_templates notification_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.notification_templates
    ADD CONSTRAINT notification_templates_pkey PRIMARY KEY (id);


-- Name: oncall_assignment_rules oncall_assignment_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.oncall_assignment_rules
    ADD CONSTRAINT oncall_assignment_rules_pkey PRIMARY KEY (id);


-- Name: oncall_escalation_policies oncall_escalation_policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.oncall_escalation_policies
    ADD CONSTRAINT oncall_escalation_policies_pkey PRIMARY KEY (id);


-- Name: oncall_rotations oncall_rotations_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.oncall_rotations
    ADD CONSTRAINT oncall_rotations_pkey PRIMARY KEY (id);


-- Name: oncall_schedules oncall_schedules_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.oncall_schedules
    ADD CONSTRAINT oncall_schedules_pkey PRIMARY KEY (id);


-- Name: operation_logs operation_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.operation_logs
    ADD CONSTRAINT operation_logs_pkey PRIMARY KEY (id);


-- Name: personal_access_tokens personal_access_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.personal_access_tokens
    ADD CONSTRAINT personal_access_tokens_pkey PRIMARY KEY (id);


-- Name: policies policies_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.policies
    ADD CONSTRAINT policies_pkey PRIMARY KEY (id);


-- Name: port_forward_sessions port_forward_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.port_forward_sessions
    ADD CONSTRAINT port_forward_sessions_pkey PRIMARY KEY (session_id);


-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);


-- Name: projects projects_slug_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_slug_key UNIQUE (slug);


-- Name: registry_connections registry_connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.registry_connections
    ADD CONSTRAINT registry_connections_pkey PRIMARY KEY (id);


-- Name: release_bundles release_bundles_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_bundles
    ADD CONSTRAINT release_bundles_pkey PRIMARY KEY (id);


-- Name: release_targets release_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_targets
    ADD CONSTRAINT release_targets_pkey PRIMARY KEY (id);


-- Name: roles roles_name_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_name_key UNIQUE (name);


-- Name: roles roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);


-- Name: scope_grants scope_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.scope_grants
    ADD CONSTRAINT scope_grants_pkey PRIMARY KEY (id);


-- Name: service_account_tokens service_account_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.service_account_tokens
    ADD CONSTRAINT service_account_tokens_pkey PRIMARY KEY (id);


-- Name: service_accounts service_accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.service_accounts
    ADD CONSTRAINT service_accounts_pkey PRIMARY KEY (id);


-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);


-- Name: sessions sessions_refresh_token_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_refresh_token_id_key UNIQUE (refresh_token_id);


-- Name: teams teams_name_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_name_key UNIQUE (name);


-- Name: teams teams_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_pkey PRIMARY KEY (id);


-- Name: teams teams_slug_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_slug_key UNIQUE (slug);


-- Name: user_identities user_identities_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_identities
    ADD CONSTRAINT user_identities_pkey PRIMARY KEY (id);


-- Name: user_identities user_identities_provider_type_provider_id_provider_user_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_identities
    ADD CONSTRAINT user_identities_provider_type_provider_id_provider_user_id_key UNIQUE (provider_type, provider_id, provider_user_id);


-- Name: user_password_credentials user_password_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_password_credentials
    ADD CONSTRAINT user_password_credentials_pkey PRIMARY KEY (user_id);


-- Name: user_project_bindings user_project_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_project_bindings
    ADD CONSTRAINT user_project_bindings_pkey PRIMARY KEY (id);


-- Name: user_project_bindings user_project_bindings_user_id_project_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_project_bindings
    ADD CONSTRAINT user_project_bindings_user_id_project_id_key UNIQUE (user_id, project_id);


-- Name: user_role_bindings user_role_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_role_bindings
    ADD CONSTRAINT user_role_bindings_pkey PRIMARY KEY (id);


-- Name: user_role_bindings user_role_bindings_user_id_role_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_role_bindings
    ADD CONSTRAINT user_role_bindings_user_id_role_id_key UNIQUE (user_id, role_id);


-- Name: user_team_bindings user_team_bindings_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_team_bindings
    ADD CONSTRAINT user_team_bindings_pkey PRIMARY KEY (id);


-- Name: user_team_bindings user_team_bindings_user_id_team_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_team_bindings
    ADD CONSTRAINT user_team_bindings_user_id_team_id_key UNIQUE (user_id, team_id);


-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


-- Name: users users_username_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_username_key UNIQUE (username);


-- Name: virtualization_connections virtualization_connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_connections
    ADD CONSTRAINT virtualization_connections_pkey PRIMARY KEY (id);


-- Name: virtualization_flavors virtualization_flavors_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_flavors
    ADD CONSTRAINT virtualization_flavors_pkey PRIMARY KEY (id);


-- Name: virtualization_images virtualization_images_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_images
    ADD CONSTRAINT virtualization_images_pkey PRIMARY KEY (id);


-- Name: virtualization_images virtualization_images_provider_connection_id_external_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_images
    ADD CONSTRAINT virtualization_images_provider_connection_id_external_id_key UNIQUE (provider, connection_id, external_id);


-- Name: virtualization_task_logs virtualization_task_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_task_logs
    ADD CONSTRAINT virtualization_task_logs_pkey PRIMARY KEY (id);


-- Name: virtualization_tasks virtualization_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_tasks
    ADD CONSTRAINT virtualization_tasks_pkey PRIMARY KEY (id);


-- Name: virtualization_vms virtualization_vms_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_vms
    ADD CONSTRAINT virtualization_vms_pkey PRIMARY KEY (id);


-- Name: virtualization_vms virtualization_vms_provider_connection_id_external_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_vms
    ADD CONSTRAINT virtualization_vms_provider_connection_id_external_id_key UNIQUE (provider, connection_id, external_id);


-- Name: workflow_approvals workflow_approvals_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workflow_approvals
    ADD CONSTRAINT workflow_approvals_pkey PRIMARY KEY (id);


-- Name: workflow_runs workflow_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workflow_runs
    ADD CONSTRAINT workflow_runs_pkey PRIMARY KEY (id);


-- Name: workflow_templates workflow_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workflow_templates
    ADD CONSTRAINT workflow_templates_pkey PRIMARY KEY (id);


-- Name: workflow_templates workflow_templates_template_key_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workflow_templates
    ADD CONSTRAINT workflow_templates_template_key_key UNIQUE (template_key);


-- Name: idx_ai_access_policies_client; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_access_policies_client ON public.ai_access_policies USING btree (ai_client_id);


-- Name: idx_ai_access_policies_subject; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_access_policies_subject ON public.ai_access_policies USING btree (subject_type, subject_id);


-- Name: idx_ai_agent_runs_provider_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_agent_runs_provider_status ON public.ai_agent_runs USING btree (provider_id, status);


-- Name: idx_ai_agent_runs_root_cause_run; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_agent_runs_root_cause_run ON public.ai_agent_runs USING btree (root_cause_run_id);


-- Name: idx_ai_agent_runs_session_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_agent_runs_session_created_at ON public.ai_agent_runs USING btree (session_id, created_at DESC);


-- Name: idx_ai_agent_runs_status_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_agent_runs_status_created_at ON public.ai_agent_runs USING btree (status, created_at);


-- Name: idx_ai_analysis_profiles_mode_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_analysis_profiles_mode_enabled ON public.ai_analysis_profiles USING btree (mode, enabled);


-- Name: idx_ai_automation_policies_trigger_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_automation_policies_trigger_enabled ON public.ai_automation_policies USING btree (trigger_type, enabled);


-- Name: idx_ai_clients_kind_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_clients_kind_status ON public.ai_clients USING btree (kind, status);


-- Name: idx_ai_data_sources_kind_backend_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_data_sources_kind_backend_enabled ON public.ai_data_sources USING btree (source_kind, backend_type, enabled);


-- Name: idx_ai_data_sources_type_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_data_sources_type_enabled ON public.ai_data_sources USING btree (source_type, enabled);


-- Name: idx_ai_data_sources_validation_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_data_sources_validation_status ON public.ai_data_sources USING btree (validation_status, last_validated_at DESC);


-- Name: idx_ai_gateway_approval_requests_actor; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_approval_requests_actor ON public.ai_gateway_approval_requests USING btree (actor_type, actor_id, created_at DESC);


-- Name: idx_ai_gateway_approval_requests_client; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_approval_requests_client ON public.ai_gateway_approval_requests USING btree (ai_client_id, created_at DESC);


-- Name: idx_ai_gateway_approval_requests_expires; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_approval_requests_expires ON public.ai_gateway_approval_requests USING btree (status, expires_at);


-- Name: idx_ai_gateway_approval_requests_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_approval_requests_status ON public.ai_gateway_approval_requests USING btree (status, created_at DESC);


-- Name: idx_ai_gateway_approval_requests_tool; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_approval_requests_tool ON public.ai_gateway_approval_requests USING btree (tool_name, created_at DESC);


-- Name: idx_ai_gateway_audit_logs_actor; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_audit_logs_actor ON public.ai_gateway_audit_logs USING btree (actor_type, actor_id, created_at DESC);


-- Name: idx_ai_gateway_audit_logs_client; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_audit_logs_client ON public.ai_gateway_audit_logs USING btree (ai_client_id, created_at DESC);


-- Name: idx_ai_gateway_audit_logs_tool; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_audit_logs_tool ON public.ai_gateway_audit_logs USING btree (tool_name, created_at DESC);


-- Name: idx_ai_gateway_rate_limit_counters_actor; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_counters_actor ON public.ai_gateway_rate_limit_counters USING btree (actor_type, actor_id, window_end);


-- Name: idx_ai_gateway_rate_limit_counters_client_tool; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_counters_client_tool ON public.ai_gateway_rate_limit_counters USING btree (ai_client_id, tool_name, window_end);


-- Name: idx_ai_gateway_rate_limit_counters_policy_window; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_counters_policy_window ON public.ai_gateway_rate_limit_counters USING btree (policy_id, window_start, window_end);


-- Name: idx_ai_gateway_rate_limit_counters_window_end; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_counters_window_end ON public.ai_gateway_rate_limit_counters USING btree (window_end);


-- Name: idx_ai_gateway_rate_limit_states_actor; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_states_actor ON public.ai_gateway_rate_limit_states USING btree (actor_type, actor_id, updated_at);


-- Name: idx_ai_gateway_rate_limit_states_client_tool; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_states_client_tool ON public.ai_gateway_rate_limit_states USING btree (ai_client_id, tool_name, updated_at);


-- Name: idx_ai_gateway_rate_limit_states_policy; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_states_policy ON public.ai_gateway_rate_limit_states USING btree (policy_id, updated_at);


-- Name: idx_ai_gateway_rate_limit_states_tat; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_rate_limit_states_tat ON public.ai_gateway_rate_limit_states USING btree (tat);


-- Name: idx_ai_gateway_skill_bindings_client_skill; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_skill_bindings_client_skill ON public.ai_gateway_skill_bindings USING btree (ai_client_id, skill_id);


-- Name: idx_ai_gateway_skill_bindings_subject; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_gateway_skill_bindings_subject ON public.ai_gateway_skill_bindings USING btree (subject_type, subject_id);


-- Name: idx_ai_inspection_runs_task_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_inspection_runs_task_created_at ON public.ai_inspection_runs USING btree (task_id, created_at DESC);


-- Name: idx_ai_inspection_tasks_created_by; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_inspection_tasks_created_by ON public.ai_inspection_tasks USING btree (created_by);


-- Name: idx_ai_inspection_tasks_enabled_last_run_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_inspection_tasks_enabled_last_run_at ON public.ai_inspection_tasks USING btree (enabled, last_run_at);


-- Name: idx_ai_messages_session_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_messages_session_created_at ON public.ai_messages USING btree (session_id, created_at);


-- Name: idx_ai_root_cause_runs_alert_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_root_cause_runs_alert_id ON public.ai_root_cause_runs USING btree (alert_id);


-- Name: idx_ai_root_cause_runs_cluster_namespace; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_root_cause_runs_cluster_namespace ON public.ai_root_cause_runs USING btree (cluster_id, namespace);


-- Name: idx_ai_root_cause_runs_created_by_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_root_cause_runs_created_by_updated_at ON public.ai_root_cause_runs USING btree (created_by, updated_at DESC);


-- Name: idx_ai_root_cause_runs_dedup_key; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_root_cause_runs_dedup_key ON public.ai_root_cause_runs USING btree (dedup_key);


-- Name: idx_ai_root_cause_runs_session_id_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_root_cause_runs_session_id_created_at ON public.ai_root_cause_runs USING btree (session_id, created_at DESC);


-- Name: idx_ai_root_cause_runs_trigger_type_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_root_cause_runs_trigger_type_created_at ON public.ai_root_cause_runs USING btree (trigger_type, created_at DESC);


-- Name: idx_ai_sessions_created_by_deleted_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_ai_sessions_created_by_deleted_updated_at ON public.ai_sessions USING btree (created_by, deleted_at, updated_at DESC);


-- Name: idx_alert_delivery_logs_alert_id_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_alert_delivery_logs_alert_id_created_at ON public.alert_delivery_logs USING btree (alert_id, created_at DESC);


-- Name: idx_alert_delivery_logs_status_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_alert_delivery_logs_status_created_at ON public.alert_delivery_logs USING btree (status, created_at DESC);


-- Name: idx_alert_events_status_last_seen_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_alert_events_status_last_seen_at ON public.alert_events USING btree (status, last_seen_at DESC);


-- Name: idx_alert_integrations_status_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_alert_integrations_status_updated_at ON public.alert_integrations USING btree (status, updated_at DESC);


-- Name: idx_alert_integrations_type_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_alert_integrations_type_enabled ON public.alert_integrations USING btree (integration_type, enabled, updated_at DESC);


-- Name: idx_alert_rules_enabled_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_alert_rules_enabled_updated_at ON public.alert_rules USING btree (enabled, updated_at DESC);


-- Name: idx_alert_silences_enabled_time; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_alert_silences_enabled_time ON public.alert_silences USING btree (enabled, starts_at, ends_at);


-- Name: idx_announcement_receipts_announcement_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_announcement_receipts_announcement_id ON public.announcement_receipts USING btree (announcement_id);


-- Name: idx_announcement_receipts_user_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_announcement_receipts_user_id ON public.announcement_receipts USING btree (user_id);


-- Name: idx_application_build_sources_application_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_application_build_sources_application_id ON public.application_build_sources USING btree (application_id);


-- Name: idx_application_environments_application_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_application_environments_application_id ON public.application_environments USING btree (application_id);


-- Name: idx_application_environments_environment_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_application_environments_environment_id ON public.application_environments USING btree (environment_id);


-- Name: idx_application_service_containers_service_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_application_service_containers_service_id ON public.application_service_containers USING btree (service_id);


-- Name: idx_application_services_application_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_application_services_application_id ON public.application_services USING btree (application_id);


-- Name: idx_application_services_build_source_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_application_services_build_source_id ON public.application_services USING btree (build_source_id);


-- Name: idx_applications_business_line_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_applications_business_line_id ON public.applications USING btree (business_line_id);


-- Name: idx_applications_group_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_applications_group_enabled ON public.applications USING btree (app_group, enabled);


-- Name: idx_audit_logs_actor_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_audit_logs_actor_id ON public.audit_logs USING btree (actor_id);


-- Name: idx_audit_logs_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_audit_logs_created_at ON public.audit_logs USING btree (created_at DESC);


-- Name: idx_auth_ephemeral_tokens_kind_expires_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_auth_ephemeral_tokens_kind_expires_at ON public.auth_ephemeral_tokens USING btree (kind, expires_at);


-- Name: idx_build_records_project_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_build_records_project_created_at ON public.build_records USING btree (project_id, created_at DESC);


-- Name: idx_clusters_environment; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_clusters_environment ON public.clusters USING btree (environment);


-- Name: idx_delivery_blueprints_enabled_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_delivery_blueprints_enabled_updated_at ON public.delivery_blueprints USING btree (enabled, updated_at DESC);


-- Name: idx_delivery_environments_key_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_delivery_environments_key_enabled ON public.delivery_environments USING btree (environment_key, enabled);


-- Name: idx_docker_hosts_agent; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_hosts_agent ON public.docker_hosts USING btree (agent_id);


-- Name: idx_docker_hosts_architecture; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_hosts_architecture ON public.docker_hosts USING btree (architecture);


-- Name: idx_docker_hosts_environment; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_hosts_environment ON public.docker_hosts USING btree (environment);


-- Name: idx_docker_hosts_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_hosts_status ON public.docker_hosts USING btree (status);


-- Name: idx_docker_hosts_vm; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_hosts_vm ON public.docker_hosts USING btree (vm_id);


-- Name: idx_docker_operation_logs_created; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operation_logs_created ON public.docker_operation_logs USING btree (created_at);


-- Name: idx_docker_operation_logs_operation; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operation_logs_operation ON public.docker_operation_logs USING btree (operation_id);


-- Name: idx_docker_operations_created; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operations_created ON public.docker_operations USING btree (created_at);


-- Name: idx_docker_operations_host; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operations_host ON public.docker_operations USING btree (host_id);


-- Name: idx_docker_operations_kind; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operations_kind ON public.docker_operations USING btree (operation_kind);


-- Name: idx_docker_operations_project; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operations_project ON public.docker_operations USING btree (project_id);


-- Name: idx_docker_operations_service; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operations_service ON public.docker_operations USING btree (service_id);


-- Name: idx_docker_operations_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_operations_status ON public.docker_operations USING btree (status);


-- Name: idx_docker_ports_domain; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_ports_domain ON public.docker_port_mappings USING btree (domain_name);


-- Name: idx_docker_ports_host; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_ports_host ON public.docker_port_mappings USING btree (host_id);


-- Name: idx_docker_ports_project; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_ports_project ON public.docker_port_mappings USING btree (project_id);


-- Name: idx_docker_ports_service; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_ports_service ON public.docker_port_mappings USING btree (service_id);


-- Name: idx_docker_ports_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_ports_status ON public.docker_port_mappings USING btree (status);


-- Name: idx_docker_ports_unique_active; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_docker_ports_unique_active ON public.docker_port_mappings USING btree (host_id, COALESCE(host_ip, ''::text), host_port, protocol) WHERE (status <> 'released'::text);


-- Name: idx_docker_ports_unique_active_domain; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_docker_ports_unique_active_domain ON public.docker_port_mappings USING btree (lower(domain_name)) WHERE ((status <> 'released'::text) AND (domain_name IS NOT NULL) AND (domain_name <> ''::text));


-- Name: idx_docker_projects_environment; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_projects_environment ON public.docker_projects USING btree (environment);


-- Name: idx_docker_projects_expires; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_projects_expires ON public.docker_projects USING btree (expires_at);


-- Name: idx_docker_projects_host; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_projects_host ON public.docker_projects USING btree (host_id);


-- Name: idx_docker_projects_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_projects_status ON public.docker_projects USING btree (status);


-- Name: idx_docker_services_host; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_services_host ON public.docker_services USING btree (host_id);


-- Name: idx_docker_services_project; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_services_project ON public.docker_services USING btree (project_id);


-- Name: idx_docker_services_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_services_status ON public.docker_services USING btree (status);


-- Name: idx_docker_templates_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_templates_enabled ON public.docker_templates USING btree (enabled);


-- Name: idx_docker_templates_kind; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_docker_templates_kind ON public.docker_templates USING btree (template_kind);


-- Name: idx_event_stream_occurred_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_event_stream_occurred_at ON public.event_stream USING btree (occurred_at DESC);


-- Name: idx_execution_artifacts_application_environment_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_artifacts_application_environment_id ON public.execution_artifacts USING btree (application_environment_id);


-- Name: idx_execution_artifacts_application_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_artifacts_application_id ON public.execution_artifacts USING btree (application_id);


-- Name: idx_execution_artifacts_bundle_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_artifacts_bundle_id ON public.execution_artifacts USING btree (release_bundle_id);


-- Name: idx_execution_artifacts_task_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_artifacts_task_id ON public.execution_artifacts USING btree (execution_task_id);


-- Name: idx_execution_callbacks_execution_task_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_callbacks_execution_task_id ON public.execution_callbacks USING btree (execution_task_id);


-- Name: idx_execution_logs_execution_task_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_logs_execution_task_id ON public.execution_logs USING btree (execution_task_id);


-- Name: idx_execution_tasks_application_environment_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_tasks_application_environment_id ON public.execution_tasks USING btree (application_environment_id);


-- Name: idx_execution_tasks_application_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_tasks_application_id ON public.execution_tasks USING btree (application_id);


-- Name: idx_execution_tasks_callback_token; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_execution_tasks_callback_token ON public.execution_tasks USING btree (callback_token) WHERE (callback_token IS NOT NULL);


-- Name: idx_execution_tasks_release_bundle_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_tasks_release_bundle_id ON public.execution_tasks USING btree (release_bundle_id);


-- Name: idx_execution_tasks_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_execution_tasks_status ON public.execution_tasks USING btree (status);


-- Name: idx_healing_policies_enabled_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_healing_policies_enabled_updated_at ON public.healing_policies USING btree (enabled, updated_at DESC);


-- Name: idx_healing_runs_status_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_healing_runs_status_updated_at ON public.healing_runs USING btree (status, updated_at DESC);


-- Name: idx_mcp_tool_grants_client_tool; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_mcp_tool_grants_client_tool ON public.mcp_tool_grants USING btree (ai_client_id, tool_name);


-- Name: idx_mcp_tool_grants_subject; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_mcp_tool_grants_subject ON public.mcp_tool_grants USING btree (subject_type, subject_id);


-- Name: idx_notification_policies_enabled_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_notification_policies_enabled_updated_at ON public.notification_policies USING btree (enabled, updated_at DESC);


-- Name: idx_notification_templates_enabled_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_notification_templates_enabled_updated_at ON public.notification_templates USING btree (enabled, updated_at DESC);


-- Name: idx_oncall_assignment_rules_alert_scope; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_oncall_assignment_rules_alert_scope ON public.oncall_assignment_rules USING btree (alert_category, severity, service, enabled, priority DESC);


-- Name: idx_oncall_assignment_rules_business_role; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_oncall_assignment_rules_business_role ON public.oncall_assignment_rules USING btree (business_line_id, role, enabled, priority DESC);


-- Name: idx_oncall_assignment_rules_integration_route; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_oncall_assignment_rules_integration_route ON public.oncall_assignment_rules USING btree (integration_type, integration_id, enabled, route_order);


-- Name: idx_oncall_escalation_policies_enabled_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_oncall_escalation_policies_enabled_updated_at ON public.oncall_escalation_policies USING btree (enabled, updated_at DESC);


-- Name: idx_oncall_rotations_schedule_id_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_oncall_rotations_schedule_id_updated_at ON public.oncall_rotations USING btree (schedule_id, updated_at DESC);


-- Name: idx_oncall_schedules_enabled_updated_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_oncall_schedules_enabled_updated_at ON public.oncall_schedules USING btree (enabled, updated_at DESC);


-- Name: idx_personal_access_tokens_active; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_personal_access_tokens_active ON public.personal_access_tokens USING btree (user_id, expires_at) WHERE (revoked_at IS NULL);


-- Name: idx_personal_access_tokens_token_hash; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_personal_access_tokens_token_hash ON public.personal_access_tokens USING btree (token_hash);


-- Name: idx_personal_access_tokens_user_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_personal_access_tokens_user_id ON public.personal_access_tokens USING btree (user_id);


-- Name: idx_port_forward_sessions_cluster; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_port_forward_sessions_cluster ON public.port_forward_sessions USING btree (cluster_id);


-- Name: idx_registry_connections_type_name; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_registry_connections_type_name ON public.registry_connections USING btree (registry_type, name);


-- Name: idx_release_bundles_application_environment_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_release_bundles_application_environment_id ON public.release_bundles USING btree (application_environment_id);


-- Name: idx_release_bundles_application_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_release_bundles_application_id ON public.release_bundles USING btree (application_id);


-- Name: idx_release_targets_application_environment_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_release_targets_application_environment_id ON public.release_targets USING btree (application_environment_id);


-- Name: idx_release_targets_cluster_namespace_workload; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_release_targets_cluster_namespace_workload ON public.release_targets USING btree (cluster_id, namespace, workload_kind, workload_name);


-- Name: idx_scope_grants_business_line_role; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_scope_grants_business_line_role ON public.scope_grants USING btree (business_line_id, role);


-- Name: idx_scope_grants_subject; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_scope_grants_subject ON public.scope_grants USING btree (subject_type, subject_id);


-- Name: idx_service_account_tokens_account; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_service_account_tokens_account ON public.service_account_tokens USING btree (service_account_id);


-- Name: idx_service_account_tokens_active; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_service_account_tokens_active ON public.service_account_tokens USING btree (service_account_id, expires_at) WHERE (revoked_at IS NULL);


-- Name: idx_service_account_tokens_token_hash; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_service_account_tokens_token_hash ON public.service_account_tokens USING btree (token_hash);


-- Name: idx_service_accounts_owner; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_service_accounts_owner ON public.service_accounts USING btree (owner_user_id);


-- Name: idx_service_accounts_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_service_accounts_status ON public.service_accounts USING btree (status);


-- Name: idx_sessions_user_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_sessions_user_id ON public.sessions USING btree (user_id);


-- Name: idx_teams_org_path; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_teams_org_path ON public.teams USING btree (org_path);


-- Name: idx_teams_parent_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_teams_parent_id ON public.teams USING btree (parent_id);


-- Name: idx_teams_source_external_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_teams_source_external_id ON public.teams USING btree (source, external_id);


-- Name: idx_user_role_bindings_user_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_user_role_bindings_user_id ON public.user_role_bindings USING btree (user_id);


-- Name: idx_user_role_bindings_user_source_provider; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_user_role_bindings_user_source_provider ON public.user_role_bindings USING btree (user_id, source, provider_id);


-- Name: idx_user_team_bindings_user_source_provider; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_user_team_bindings_user_source_provider ON public.user_team_bindings USING btree (user_id, source, provider_id);


-- Name: idx_virtualization_connections_cluster; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_connections_cluster ON public.virtualization_connections USING btree (kubernetes_cluster_id);


-- Name: idx_virtualization_connections_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_connections_enabled ON public.virtualization_connections USING btree (enabled);


-- Name: idx_virtualization_connections_provider; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_connections_provider ON public.virtualization_connections USING btree (provider);


-- Name: idx_virtualization_flavors_connection; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_flavors_connection ON public.virtualization_flavors USING btree (connection_id);


-- Name: idx_virtualization_flavors_connection_unique; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_virtualization_flavors_connection_unique ON public.virtualization_flavors USING btree (provider, connection_id, external_id) WHERE (connection_id IS NOT NULL);


-- Name: idx_virtualization_flavors_global_unique; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_virtualization_flavors_global_unique ON public.virtualization_flavors USING btree (provider, external_id) WHERE (connection_id IS NULL);


-- Name: idx_virtualization_flavors_last_seen; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_flavors_last_seen ON public.virtualization_flavors USING btree (last_seen_at);


-- Name: idx_virtualization_flavors_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_flavors_status ON public.virtualization_flavors USING btree (status);


-- Name: idx_virtualization_images_connection; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_images_connection ON public.virtualization_images USING btree (connection_id);


-- Name: idx_virtualization_images_last_seen; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_images_last_seen ON public.virtualization_images USING btree (last_seen_at);


-- Name: idx_virtualization_images_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_images_status ON public.virtualization_images USING btree (status);


-- Name: idx_virtualization_task_logs_created; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_task_logs_created ON public.virtualization_task_logs USING btree (created_at);


-- Name: idx_virtualization_task_logs_task; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_task_logs_task ON public.virtualization_task_logs USING btree (task_id);


-- Name: idx_virtualization_tasks_claim; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_tasks_claim ON public.virtualization_tasks USING btree (status, created_at) WHERE (status = 'queued'::text);


-- Name: idx_virtualization_tasks_connection; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_tasks_connection ON public.virtualization_tasks USING btree (connection_id);


-- Name: idx_virtualization_tasks_created; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_tasks_created ON public.virtualization_tasks USING btree (created_at);


-- Name: idx_virtualization_tasks_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_tasks_status ON public.virtualization_tasks USING btree (status);


-- Name: idx_virtualization_tasks_timeout; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_tasks_timeout ON public.virtualization_tasks USING btree (status, last_heartbeat_at, started_at) WHERE (status = 'running'::text);


-- Name: idx_virtualization_tasks_vm; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_tasks_vm ON public.virtualization_tasks USING btree (vm_id);


-- Name: idx_virtualization_vms_connection; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_vms_connection ON public.virtualization_vms USING btree (connection_id);


-- Name: idx_virtualization_vms_last_seen; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_vms_last_seen ON public.virtualization_vms USING btree (last_seen_at);


-- Name: idx_virtualization_vms_namespace; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_vms_namespace ON public.virtualization_vms USING btree (namespace);


-- Name: idx_virtualization_vms_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_virtualization_vms_status ON public.virtualization_vms USING btree (status);


-- Name: idx_workflow_approvals_workflow_run_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_workflow_approvals_workflow_run_id ON public.workflow_approvals USING btree (workflow_run_id);


-- Name: idx_workflow_runs_application_created_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_workflow_runs_application_created_at ON public.workflow_runs USING btree (application_id, created_at DESC);


-- Name: idx_workflow_templates_key_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_workflow_templates_key_enabled ON public.workflow_templates USING btree (template_key, enabled);


-- Name: ai_access_policies ai_access_policies_ai_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_access_policies
    ADD CONSTRAINT ai_access_policies_ai_client_id_fkey FOREIGN KEY (ai_client_id) REFERENCES public.ai_clients(id) ON DELETE CASCADE;


-- Name: ai_agent_runs ai_agent_runs_root_cause_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_agent_runs
    ADD CONSTRAINT ai_agent_runs_root_cause_run_id_fkey FOREIGN KEY (root_cause_run_id) REFERENCES public.ai_root_cause_runs(id) ON DELETE SET NULL;


-- Name: ai_agent_runs ai_agent_runs_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_agent_runs
    ADD CONSTRAINT ai_agent_runs_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.ai_sessions(id) ON DELETE SET NULL;


-- Name: ai_automation_policies ai_automation_policies_analysis_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_automation_policies
    ADD CONSTRAINT ai_automation_policies_analysis_profile_id_fkey FOREIGN KEY (analysis_profile_id) REFERENCES public.ai_analysis_profiles(id);


-- Name: ai_gateway_skill_bindings ai_gateway_skill_bindings_ai_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_gateway_skill_bindings
    ADD CONSTRAINT ai_gateway_skill_bindings_ai_client_id_fkey FOREIGN KEY (ai_client_id) REFERENCES public.ai_clients(id) ON DELETE CASCADE;


-- Name: ai_inspection_runs ai_inspection_runs_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_inspection_runs
    ADD CONSTRAINT ai_inspection_runs_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.ai_inspection_tasks(id) ON DELETE CASCADE;


-- Name: ai_messages ai_messages_session_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.ai_messages
    ADD CONSTRAINT ai_messages_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.ai_sessions(id) ON DELETE CASCADE;


-- Name: alert_rule_runs alert_rule_runs_rule_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.alert_rule_runs
    ADD CONSTRAINT alert_rule_runs_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES public.alert_rules(id) ON DELETE CASCADE;


-- Name: announcement_receipts announcement_receipts_announcement_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.announcement_receipts
    ADD CONSTRAINT announcement_receipts_announcement_id_fkey FOREIGN KEY (announcement_id) REFERENCES public.announcements(id) ON DELETE CASCADE;


-- Name: announcement_receipts announcement_receipts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.announcement_receipts
    ADD CONSTRAINT announcement_receipts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: application_build_sources application_build_sources_application_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_build_sources
    ADD CONSTRAINT application_build_sources_application_id_fkey FOREIGN KEY (application_id) REFERENCES public.applications(id) ON DELETE CASCADE;


-- Name: application_environments application_environments_application_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_environments
    ADD CONSTRAINT application_environments_application_id_fkey FOREIGN KEY (application_id) REFERENCES public.applications(id) ON DELETE CASCADE;


-- Name: application_service_containers application_service_containers_service_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_service_containers
    ADD CONSTRAINT application_service_containers_service_id_fkey FOREIGN KEY (service_id) REFERENCES public.application_services(id) ON DELETE CASCADE;


-- Name: application_services application_services_application_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.application_services
    ADD CONSTRAINT application_services_application_id_fkey FOREIGN KEY (application_id) REFERENCES public.applications(id) ON DELETE CASCADE;


-- Name: cluster_credentials_meta cluster_credentials_meta_cluster_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.cluster_credentials_meta
    ADD CONSTRAINT cluster_credentials_meta_cluster_id_fkey FOREIGN KEY (cluster_id) REFERENCES public.clusters(id);


-- Name: docker_operation_logs docker_operation_logs_operation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_operation_logs
    ADD CONSTRAINT docker_operation_logs_operation_id_fkey FOREIGN KEY (operation_id) REFERENCES public.docker_operations(id) ON DELETE CASCADE;


-- Name: docker_operations docker_operations_host_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_operations
    ADD CONSTRAINT docker_operations_host_id_fkey FOREIGN KEY (host_id) REFERENCES public.docker_hosts(id) ON DELETE SET NULL;


-- Name: docker_operations docker_operations_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_operations
    ADD CONSTRAINT docker_operations_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.docker_projects(id) ON DELETE SET NULL;


-- Name: docker_operations docker_operations_service_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_operations
    ADD CONSTRAINT docker_operations_service_id_fkey FOREIGN KEY (service_id) REFERENCES public.docker_services(id) ON DELETE SET NULL;


-- Name: docker_port_mappings docker_port_mappings_host_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_port_mappings
    ADD CONSTRAINT docker_port_mappings_host_id_fkey FOREIGN KEY (host_id) REFERENCES public.docker_hosts(id) ON DELETE CASCADE;


-- Name: docker_port_mappings docker_port_mappings_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_port_mappings
    ADD CONSTRAINT docker_port_mappings_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.docker_projects(id) ON DELETE SET NULL;


-- Name: docker_port_mappings docker_port_mappings_service_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_port_mappings
    ADD CONSTRAINT docker_port_mappings_service_id_fkey FOREIGN KEY (service_id) REFERENCES public.docker_services(id) ON DELETE SET NULL;


-- Name: docker_projects docker_projects_host_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_projects
    ADD CONSTRAINT docker_projects_host_id_fkey FOREIGN KEY (host_id) REFERENCES public.docker_hosts(id) ON DELETE CASCADE;


-- Name: docker_services docker_services_host_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_services
    ADD CONSTRAINT docker_services_host_id_fkey FOREIGN KEY (host_id) REFERENCES public.docker_hosts(id) ON DELETE CASCADE;


-- Name: docker_services docker_services_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.docker_services
    ADD CONSTRAINT docker_services_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.docker_projects(id) ON DELETE CASCADE;


-- Name: execution_artifacts execution_artifacts_application_environment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_artifacts
    ADD CONSTRAINT execution_artifacts_application_environment_id_fkey FOREIGN KEY (application_environment_id) REFERENCES public.application_environments(id) ON DELETE SET NULL;


-- Name: execution_artifacts execution_artifacts_application_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_artifacts
    ADD CONSTRAINT execution_artifacts_application_id_fkey FOREIGN KEY (application_id) REFERENCES public.applications(id) ON DELETE CASCADE;


-- Name: execution_artifacts execution_artifacts_execution_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_artifacts
    ADD CONSTRAINT execution_artifacts_execution_task_id_fkey FOREIGN KEY (execution_task_id) REFERENCES public.execution_tasks(id) ON DELETE CASCADE;


-- Name: execution_artifacts execution_artifacts_release_bundle_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_artifacts
    ADD CONSTRAINT execution_artifacts_release_bundle_id_fkey FOREIGN KEY (release_bundle_id) REFERENCES public.release_bundles(id) ON DELETE SET NULL;


-- Name: execution_callbacks execution_callbacks_execution_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_callbacks
    ADD CONSTRAINT execution_callbacks_execution_task_id_fkey FOREIGN KEY (execution_task_id) REFERENCES public.execution_tasks(id) ON DELETE CASCADE;


-- Name: execution_logs execution_logs_execution_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_logs
    ADD CONSTRAINT execution_logs_execution_task_id_fkey FOREIGN KEY (execution_task_id) REFERENCES public.execution_tasks(id) ON DELETE CASCADE;


-- Name: execution_tasks execution_tasks_application_environment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_tasks
    ADD CONSTRAINT execution_tasks_application_environment_id_fkey FOREIGN KEY (application_environment_id) REFERENCES public.application_environments(id) ON DELETE SET NULL;


-- Name: execution_tasks execution_tasks_application_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_tasks
    ADD CONSTRAINT execution_tasks_application_id_fkey FOREIGN KEY (application_id) REFERENCES public.applications(id) ON DELETE CASCADE;


-- Name: execution_tasks execution_tasks_release_bundle_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.execution_tasks
    ADD CONSTRAINT execution_tasks_release_bundle_id_fkey FOREIGN KEY (release_bundle_id) REFERENCES public.release_bundles(id) ON DELETE SET NULL;


-- Name: healing_runs healing_runs_policy_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.healing_runs
    ADD CONSTRAINT healing_runs_policy_id_fkey FOREIGN KEY (policy_id) REFERENCES public.healing_policies(id) ON DELETE CASCADE;


-- Name: mcp_tool_grants mcp_tool_grants_ai_client_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.mcp_tool_grants
    ADD CONSTRAINT mcp_tool_grants_ai_client_id_fkey FOREIGN KEY (ai_client_id) REFERENCES public.ai_clients(id) ON DELETE CASCADE;


-- Name: menu_role_bindings menu_role_bindings_menu_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.menu_role_bindings
    ADD CONSTRAINT menu_role_bindings_menu_id_fkey FOREIGN KEY (menu_id) REFERENCES public.menus(id) ON DELETE CASCADE;


-- Name: menu_role_bindings menu_role_bindings_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.menu_role_bindings
    ADD CONSTRAINT menu_role_bindings_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


-- Name: menus menus_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.menus
    ADD CONSTRAINT menus_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.menus(id) ON DELETE CASCADE;


-- Name: oncall_rotations oncall_rotations_schedule_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.oncall_rotations
    ADD CONSTRAINT oncall_rotations_schedule_id_fkey FOREIGN KEY (schedule_id) REFERENCES public.oncall_schedules(id) ON DELETE CASCADE;


-- Name: personal_access_tokens personal_access_tokens_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.personal_access_tokens
    ADD CONSTRAINT personal_access_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: projects projects_team_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_team_id_fkey FOREIGN KEY (team_id) REFERENCES public.teams(id);


-- Name: release_bundles release_bundles_application_environment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_bundles
    ADD CONSTRAINT release_bundles_application_environment_id_fkey FOREIGN KEY (application_environment_id) REFERENCES public.application_environments(id) ON DELETE SET NULL;


-- Name: release_bundles release_bundles_application_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_bundles
    ADD CONSTRAINT release_bundles_application_id_fkey FOREIGN KEY (application_id) REFERENCES public.applications(id) ON DELETE CASCADE;


-- Name: release_targets release_targets_application_environment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_targets
    ADD CONSTRAINT release_targets_application_environment_id_fkey FOREIGN KEY (application_environment_id) REFERENCES public.application_environments(id) ON DELETE CASCADE;


-- Name: release_targets release_targets_cluster_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_targets
    ADD CONSTRAINT release_targets_cluster_id_fkey FOREIGN KEY (cluster_id) REFERENCES public.clusters(id);


-- Name: service_account_tokens service_account_tokens_service_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.service_account_tokens
    ADD CONSTRAINT service_account_tokens_service_account_id_fkey FOREIGN KEY (service_account_id) REFERENCES public.service_accounts(id) ON DELETE CASCADE;


-- Name: service_accounts service_accounts_owner_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.service_accounts
    ADD CONSTRAINT service_accounts_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id) ON DELETE SET NULL;


-- Name: sessions sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: teams teams_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.teams
    ADD CONSTRAINT teams_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.teams(id) ON DELETE SET NULL;


-- Name: user_identities user_identities_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_identities
    ADD CONSTRAINT user_identities_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: user_password_credentials user_password_credentials_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_password_credentials
    ADD CONSTRAINT user_password_credentials_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: user_project_bindings user_project_bindings_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_project_bindings
    ADD CONSTRAINT user_project_bindings_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


-- Name: user_project_bindings user_project_bindings_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_project_bindings
    ADD CONSTRAINT user_project_bindings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: user_role_bindings user_role_bindings_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_role_bindings
    ADD CONSTRAINT user_role_bindings_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;


-- Name: user_role_bindings user_role_bindings_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_role_bindings
    ADD CONSTRAINT user_role_bindings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: user_team_bindings user_team_bindings_team_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_team_bindings
    ADD CONSTRAINT user_team_bindings_team_id_fkey FOREIGN KEY (team_id) REFERENCES public.teams(id) ON DELETE CASCADE;


-- Name: user_team_bindings user_team_bindings_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_team_bindings
    ADD CONSTRAINT user_team_bindings_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


-- Name: virtualization_flavors virtualization_flavors_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_flavors
    ADD CONSTRAINT virtualization_flavors_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.virtualization_connections(id) ON DELETE CASCADE;


-- Name: virtualization_images virtualization_images_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_images
    ADD CONSTRAINT virtualization_images_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.virtualization_connections(id) ON DELETE CASCADE;


-- Name: virtualization_task_logs virtualization_task_logs_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_task_logs
    ADD CONSTRAINT virtualization_task_logs_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.virtualization_tasks(id) ON DELETE CASCADE;


-- Name: virtualization_tasks virtualization_tasks_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_tasks
    ADD CONSTRAINT virtualization_tasks_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.virtualization_connections(id) ON DELETE SET NULL;


-- Name: virtualization_tasks virtualization_tasks_vm_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_tasks
    ADD CONSTRAINT virtualization_tasks_vm_id_fkey FOREIGN KEY (vm_id) REFERENCES public.virtualization_vms(id) ON DELETE SET NULL;


-- Name: virtualization_vms virtualization_vms_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.virtualization_vms
    ADD CONSTRAINT virtualization_vms_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.virtualization_connections(id) ON DELETE CASCADE;


-- Name: workflow_approvals workflow_approvals_workflow_run_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workflow_approvals
    ADD CONSTRAINT workflow_approvals_workflow_run_id_fkey FOREIGN KEY (workflow_run_id) REFERENCES public.workflow_runs(id) ON DELETE CASCADE;


-- PostgreSQL database dump complete



COMMIT;
