export type ConnectorStatus = "planned" | "experimental" | "stable";

export type ConnectorLifecycleState =
  | "created"
  | "configured"
  | "started"
  | "stopped";

export interface ConnectorSecretRef {
  env: string;
  required: boolean;
  purpose: string;
}

export interface ConnectorConfigField {
  name: string;
  type: "string" | "boolean" | "number" | "secret";
  required: boolean;
  description: string;
  env?: string;
  default?: string | number | boolean;
}

export interface ConnectorCapability {
  name: string;
  direction: "inbound" | "outbound" | "bidirectional";
  description: string;
}

export interface ConnectorPermission {
  key: string;
  description: string;
  required: boolean;
}

export interface ConnectorActionDefinition {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
  credentialProtocol?: typeof CONNECTOR_ACTION_PROTOCOL;
  credentialAliases?: string[];
}

export const CONNECTOR_ACTION_PROTOCOL = "opensoha.dev/connector-action/v1";

export interface ConnectorCredentialDelivery {
  expiresAt: string;
  entries: Record<string, { reference: string; value: string }>;
}

export interface ConnectorManifest {
  id: string;
  name: string;
  version: string;
  status: ConnectorStatus;
  description: string;
  runtime: {
    language: "typescript";
    node: string;
    entrypoint: string;
  };
  lifecycle: ConnectorLifecycleState[];
  config: ConnectorConfigField[];
  secrets: ConnectorSecretRef[];
  capabilities: ConnectorCapability[];
  permissions: ConnectorPermission[];
  events: string[];
  actions: ConnectorActionDefinition[];
}

export interface ConnectorHealth {
  status: "ok" | "degraded" | "stopped";
  message: string;
  checkedAt: string;
  details?: Record<string, unknown>;
}

export interface ConnectorContext {
  connectorId: string;
  logger?: ConnectorLogger;
  now?: () => Date;
}

export interface ConnectorLogger {
  debug(message: string, fields?: Record<string, unknown>): void;
  info(message: string, fields?: Record<string, unknown>): void;
  warn(message: string, fields?: Record<string, unknown>): void;
  error(message: string, fields?: Record<string, unknown>): void;
}

export interface ConnectorEvent {
  id: string;
  type: string;
  source: string;
  occurredAt: string;
  subject?: string;
  payload: Record<string, unknown>;
}

export interface ConnectorActionRequest<Input = Record<string, unknown>> {
  action: string;
  input: Input;
  event?: ConnectorEvent;
  requestId?: string;
  credentials?: ConnectorCredentialDelivery;
  signal?: AbortSignal;
}

export interface ConnectorActionResult<Output = Record<string, unknown>> {
  ok: boolean;
  output?: Output;
  error?: {
    code: string;
    message: string;
    retryable: boolean;
  };
}

export interface ConnectorWebhookRequest {
  headers: Record<string, string | string[] | undefined>;
  rawBody: string | Uint8Array;
}

export interface ConnectorWebhookResponse {
  status: number;
  headers?: Record<string, string>;
  body: Record<string, unknown> | string;
  events?: ConnectorEvent[];
  actions?: ConnectorActionResult[];
}

export interface Connector<TConfig> {
  readonly id: string;
  readonly manifest: ConnectorManifest;
  readonly state: ConnectorLifecycleState;
  configure(config: TConfig): Promise<void> | void;
  start(context: ConnectorContext): Promise<void> | void;
  stop(): Promise<void> | void;
  health(): Promise<ConnectorHealth>;
  handleWebhook(request: ConnectorWebhookRequest): Promise<ConnectorWebhookResponse>;
  dispatchAction(request: ConnectorActionRequest): Promise<ConnectorActionResult>;
}
