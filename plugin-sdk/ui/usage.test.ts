import { attachPluginFrame, preparePluginFrame, connectPlugin, type PluginContext } from "./index.js";
declare const frame: HTMLIFrameElement;
const context: PluginContext = { theme: "light", locale: "en-US", tokens: {} };
const activation = preparePluginFrame("https://soha.example/plugins/panel/index.html");
frame.src = activation.url;
const channel = attachPluginFrame({
  frame, documentNonce: activation.documentNonce, pluginId: "example.panel", version: "1.0", context,
  actions: ["probe"], getActiveVersion: () => "1.0",
  invoke: (_action, value, call) => call.signal.aborted ? null : value,
});
channel.updateContext(context);
const page = connectPlugin({ pluginId: "example.panel", version: "1.0", onContext: c => { document.documentElement.lang = c.locale; } });
void page.invokePluginAction("probe", { bucket: "demo" });
// @ts-expect-error executable values cannot cross the bridge
void page.invokePluginAction("probe", () => {});
