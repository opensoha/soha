export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
export interface PluginContext {
  theme: "light" | "dark";
  locale: "zh-CN" | "en-US";
  tokens: Record<string, string>;
}
export interface PageOptions {
  pluginId: string;
  version: string;
  parentOrigin?: string;
  onContext?(context: PluginContext): void;
  timeoutMs?: number;
  window?: Window;
}
export function connectPlugin(options: PageOptions): {
  invokePluginAction(actionId: string, payload?: Json): Promise<Json>;
  dispose(): void;
};
export function preparePluginFrame(entryURL: string | URL): { url: string; documentNonce: string };
export interface HostOptions {
  documentNonce: string;
  frame: HTMLIFrameElement;
  pluginId: string;
  version: string;
  context: PluginContext;
  actions: Iterable<string>;
  getActiveVersion(): string | undefined;
  invoke(actionId: string, payload: Json, call: { pluginId: string; version: string; signal: AbortSignal }): Promise<Json> | Json;
  timeoutMs?: number;
}
export function attachPluginFrame(options: HostOptions): {
  updateContext(context: PluginContext): void;
  dispose(): void;
};
