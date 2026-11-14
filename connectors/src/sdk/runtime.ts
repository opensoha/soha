import type {
  Connector,
  ConnectorActionRequest,
  ConnectorActionResult,
  ConnectorContext,
  ConnectorHealth,
  ConnectorLifecycleState,
  ConnectorManifest,
  ConnectorWebhookRequest,
  ConnectorWebhookResponse
} from "./types.js";

export abstract class BaseConnector<TConfig> implements Connector<TConfig> {
  private lifecycleState: ConnectorLifecycleState = "created";
  protected config?: TConfig;
  protected context?: ConnectorContext;

  abstract readonly id: string;
  abstract readonly manifest: ConnectorManifest;

  get state(): ConnectorLifecycleState {
    return this.lifecycleState;
  }

  configure(config: TConfig): void {
    this.config = config;
    this.lifecycleState = "configured";
  }

  start(context: ConnectorContext): void {
    this.ensureConfigured();
    this.context = context;
    this.lifecycleState = "started";
  }

  stop(): void {
    this.lifecycleState = "stopped";
  }

  async health(): Promise<ConnectorHealth> {
    return {
      status: this.lifecycleState === "started" ? "ok" : "stopped",
      message: `connector is ${this.lifecycleState}`,
      checkedAt: this.now().toISOString()
    };
  }

  abstract handleWebhook(request: ConnectorWebhookRequest): Promise<ConnectorWebhookResponse>;
  abstract dispatchAction(request: ConnectorActionRequest): Promise<ConnectorActionResult>;

  protected ensureConfigured(): asserts this is this & { config: TConfig } {
    if (!this.config) {
      throw new Error(`${this.id} connector is not configured`);
    }
  }

  protected ensureStarted(): void {
    if (this.lifecycleState !== "started") {
      throw new Error(`${this.id} connector is not started`);
    }
  }

  protected setConfiguredConfig(config: TConfig): void {
    this.config = config;
    this.lifecycleState = "configured";
  }

  protected now(): Date {
    return this.context?.now?.() ?? new Date();
  }
}
