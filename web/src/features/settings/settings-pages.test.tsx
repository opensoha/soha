/** @vitest-environment jsdom */

import { act } from "react";
import type { ReactNode } from "react";
import { App as AntdApp } from "antd";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import type { PermissionSnapshot } from "@/types";
import { AISettingsPage, SettingsCenterPage } from "./settings-pages";

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: [
      "settings.identity.view",
      "settings.identity.manage",
      "settings.branding.view",
      "settings.ai.view",
      "settings.ai.manage",
      "observe.ai.view",
    ],
    visibleMenuIds: ["settings", "settings-login", "settings-branding"],
    visibleMenus: [
      { id: "settings", path: "/settings" },
      { id: "settings-login", parentId: "settings", path: "/settings/login" },
      {
        id: "settings-branding",
        parentId: "settings",
        path: "/settings/branding",
      },
    ],
  } as PermissionSnapshot,
  responses: {} as Record<string, unknown>,
  providerModels: ["gpt-5.5", "gpt-5.4-mini"],
}));

const apiGetMock = vi.hoisted(() =>
  vi.fn((path: string) =>
    Promise.resolve({ data: testState.responses[path] ?? {} }),
  ),
);
const apiPostMock = vi.hoisted(() =>
  vi.fn((path: string) => {
    if (path === "/settings/ai/provider/models") {
      return Promise.resolve({ data: { models: testState.providerModels } });
    }
    if (path === "/settings/ai/provider/test") {
      return Promise.resolve({ data: { ok: true, reply: "ok" } });
    }
    return Promise.resolve({ data: {} });
  }),
);
const apiPutMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: {} })));

vi.mock("@/features/auth/permission-snapshot", async () => {
  const actual = await vi.importActual<
    typeof import("@/features/auth/permission-snapshot")
  >("@/features/auth/permission-snapshot");
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: { data: testState.snapshot },
      isLoading: false,
    }),
  };
});

vi.mock("@/services/api-client", () => ({
  api: {
    get: apiGetMock,
    put: apiPutMock,
    post: apiPostMock,
    delete: vi.fn(),
    upload: vi.fn(),
  },
}));

vi.mock("@/components/admin-table", () => ({
  AdminTable: ({
    title,
    dataSource,
  }: {
    title?: ReactNode;
    dataSource: unknown[];
  }) => (
    <div data-testid="admin-table">
      {title ? <div>{title}</div> : null}
      <div>{`rows:${dataSource.length}`}</div>
      {dataSource.map((item, index) => (
        <div key={index}>{JSON.stringify(item)}</div>
      ))}
    </div>
  ),
}));

let containers: HTMLDivElement[] = [];
let roots: Array<ReturnType<typeof createRoot>> = [];

function setDefaultResponses() {
  testState.responses = {
    "/settings/identity": {
      providers: [
        {
          id: "oidc-default",
          name: "OIDC",
          type: "oidc",
          enabled: true,
          issuer: "https://accounts.example.com",
          clientId: "client",
          clientSecret: "secret",
          redirectUrl:
            "http://127.0.0.1:8080/api/v1/auth/login/oidc-default/callback",
          frontendRedirectUrl: "http://127.0.0.1:5173/login/callback",
          scopes: ["openid", "profile", "email"],
          defaultRoles: ["readonly"],
          userIdField: "sub",
          userNameField: "name",
          emailField: "email",
        },
      ],
      defaultProviderId: "oidc-default",
    },
    "/settings/branding": {
      appTitle: "Soha",
      sidebarTitle: "Soha",
    },
    "/settings/ai": {
      provider: {
        enabled: true,
        baseUrl: "https://api.example.com",
        apiKey: "secret",
        model: "gpt-test",
      },
      skillsRegistry: [],
    },
    "/copilot/data-sources": [],
    "/copilot/analysis-profiles": [],
    "/copilot/automation-policies": [],
    "/copilot/data-source-capabilities": [],
    "/copilot/workbench/catalog": {
      agentProviders: [
        {
          id: "internal",
          kind: "internal",
          name: "soha 内置分析",
          enabled: true,
          default: true,
          capabilities: ["root_cause"],
          supportsAsync: false,
          supportsSkills: true,
          supportsToolsets: true,
        },
        {
          id: "hermes",
          kind: "hermes",
          name: "Hermes Agent",
          description: "通过 soha agent runner 调用 Hermes CLI。",
          enabled: true,
          capabilities: ["root_cause", "delivery_failure"],
          supportsAsync: true,
          supportsSkills: true,
          supportsToolsets: true,
        },
      ],
      capabilities: [
        { id: "root_cause", name: "根因分析" },
        { id: "delivery_failure", name: "发布失败分析" },
      ],
    },
    "/copilot/agent-runs": [
      {
        id: "agent-run-1",
        providerId: "hermes",
        providerKind: "hermes",
        capabilityId: "root_cause",
        status: "running",
        claimedByAgentId: "hermes-agent-runner",
        queuedAt: "2026-06-04T10:00:00Z",
        startedAt: "2026-06-04T10:01:00Z",
        lastHeartbeatAt: "2026-06-04T10:02:00Z",
        createdAt: "2026-06-04T10:00:00Z",
        updatedAt: "2026-06-04T10:02:00Z",
      },
    ],
  };
}

async function renderWithProviders(node: ReactNode, route: string) {
  const container = document.createElement("div");
  document.body.appendChild(container);
  containers.push(container);

  const root = createRoot(container);
  roots.push(root);

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter
            initialEntries={[route]}
            future={{
              v7_startTransition: true,
              v7_relativeSplatPath: true,
            }}
          >
            {node}
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    );
  });

  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });

  return container;
}

async function fillInput(selector: string, value: string) {
  const input = document.body.querySelector(selector) as HTMLInputElement | null;
  expect(input).not.toBeNull();

  await act(async () => {
    const valueSetter = Object.getOwnPropertyDescriptor(
      window.HTMLInputElement.prototype,
      "value",
    )?.set;
    valueSetter?.call(input, value);
    input?.dispatchEvent(new Event("input", { bubbles: true }));
    input?.dispatchEvent(new Event("change", { bubbles: true }));
    await Promise.resolve();
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("settings ai page rendering", () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, "matchMedia", {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: "",
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    });

    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.stubGlobal("ResizeObserver", ResizeObserverMock);
    Object.defineProperty(window, "getComputedStyle", {
      writable: true,
      value: vi.fn().mockReturnValue({
        getPropertyValue: () => "",
        overflow: "auto",
        overflowX: "auto",
        overflowY: "auto",
      }),
    });
  });

  beforeEach(() => {
    setDefaultResponses();
    testState.providerModels = ["gpt-5.5", "gpt-5.4-mini"];
  });

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount();
      }
    });
    roots = [];
    for (const container of containers) {
      container.remove();
    }
    containers = [];
    vi.clearAllMocks();
  });

  it("does not render AI settings as a settings-center tab anymore", async () => {
    const container = await renderWithProviders(
      <SettingsCenterPage />,
      "/settings",
    );

    expect(container.textContent).toContain("设置中心");
    expect(container.textContent).not.toContain("AI 设置");
    expect(container.textContent).not.toContain("Provider Connections");
    expect(container.textContent).toContain("登陆设置");
    expect(container.textContent).toContain("品牌设置");
  });

  it("renders login settings on /settings/login", async () => {
    const container = await renderWithProviders(
      <SettingsCenterPage />,
      "/settings/login",
    );

    expect(container.textContent).toContain("登陆设置");
    expect(container.textContent).toContain("OIDC");
    expect(container.textContent).not.toContain(
      "配置 OIDC、飞书、钉钉、企业微信、OAuth2 与 SAML 登录源。",
    );
  });

  it("exposes login role and organization mapping fields", async () => {
    const container = await renderWithProviders(
      <SettingsCenterPage />,
      "/settings/login",
    );
    const addButton = Array.from(container.querySelectorAll("button")).find(
      (button) => button.textContent?.includes("新增登录源"),
    ) as HTMLButtonElement | undefined;

    expect(addButton).toBeTruthy();

    await act(async () => {
      addButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(document.body.textContent).toContain("登录补充角色");
    expect(document.body.textContent).toContain("登录补充组织");
    expect(document.body.textContent).toContain("角色字段");
    expect(document.body.textContent).toContain("组织字段");
  });

  it("renders full AI settings content under ai-workbench model settings", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      "/ai-workbench/model-settings",
    );

    expect(container.textContent).toContain("Provider Connections");
    expect(container.textContent).toContain("Agent Runtime Providers");
    expect(container.textContent).toContain("Skills Registry");
    expect(container.textContent).toContain("Data Sources");
    expect(
      container.querySelector(
        '[data-testid="ai-provider-connections-section"]',
      ),
    ).not.toBeNull();
  });

  it("surfaces Agent Runtime providers and recent Hermes runs in model settings", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      "/ai-workbench/model-settings",
    );

    expect(container.textContent).toContain("Agent Runtime Providers");
    expect(container.textContent).toContain("Hermes Agent");
    expect(container.textContent).toContain("hermes-agent-runner");
    expect(container.textContent).toContain("agent-run-1");
  });

  it("opens the provider modal with stable critical control selectors", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      "/ai-workbench/model-settings",
    );
    const addButton = container.querySelector(
      '[data-testid="ai-provider-add"]',
    ) as HTMLButtonElement | null;

    expect(addButton).not.toBeNull();

    await act(async () => {
      addButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(
      document.body.querySelector('[data-testid="ai-provider-modal"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-form"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-name"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-kind"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-base-url"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-api-key"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-model"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-model-options"]'),
    ).toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-enabled"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-actions"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-fetch-models"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-test"]'),
    ).not.toBeNull();
  });

  it("fetches provider models while keeping manual model entry available", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      "/ai-workbench/model-settings",
    );
    const addButton = container.querySelector(
      '[data-testid="ai-provider-add"]',
    ) as HTMLButtonElement | null;

    await act(async () => {
      addButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    const modelInput = document.body.querySelector(
      '[data-testid="ai-provider-model"] input, input[data-testid="ai-provider-model"]',
    ) as HTMLInputElement | null;
    expect(modelInput).not.toBeNull();
    expect(modelInput?.disabled).toBe(false);
    expect(
      document.body.querySelector('[data-testid="ai-provider-model-options"]'),
    ).toBeNull();

    await fillInput('[data-testid="ai-provider-base-url"]', "https://api.ctsn.cc/v1");
    await fillInput('[data-testid="ai-provider-api-key"]', "secret");

    const fetchButton = document.body.querySelector(
      '[data-testid="ai-provider-fetch-models"]',
    ) as HTMLButtonElement | null;

    await act(async () => {
      fetchButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(apiPostMock).toHaveBeenCalledWith("/settings/ai/provider/models", {
      provider: expect.objectContaining({
        providerKind: "openai-compatible",
        baseUrl: "https://api.ctsn.cc/v1",
        apiKey: "secret",
        model: "",
      }),
    });
    const selectedModelInput = document.body.querySelector(
      '[data-testid="ai-provider-model"] input, input[data-testid="ai-provider-model"]',
    ) as HTMLInputElement | null;
    expect(selectedModelInput?.value).toBe("gpt-5.5");
  });

  it("tests provider connectivity with a manually entered model", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      "/ai-workbench/model-settings",
    );
    const addButton = container.querySelector(
      '[data-testid="ai-provider-add"]',
    ) as HTMLButtonElement | null;

    await act(async () => {
      addButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    await fillInput('[data-testid="ai-provider-name"]', "private-gateway");
    await fillInput('[data-testid="ai-provider-base-url"]', "https://gateway.example.com/v1");
    await fillInput('[data-testid="ai-provider-api-key"]', "secret");
    await fillInput(
      '[data-testid="ai-provider-model"] input, input[data-testid="ai-provider-model"]',
      "custom-chat-model",
    );

    const testButton = document.body.querySelector(
      '[data-testid="ai-provider-test"]',
    ) as HTMLButtonElement | null;
    await act(async () => {
      testButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(apiPostMock).toHaveBeenCalledWith("/settings/ai/provider/test", {
      provider: expect.objectContaining({
        name: "private-gateway",
        providerKind: "openai-compatible",
        baseUrl: "https://gateway.example.com/v1",
        apiKey: "secret",
        model: "custom-chat-model",
      }),
      prompt: "hello",
    });
  });

  it("tests provider connectivity with the selected fetched model", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      "/ai-workbench/model-settings",
    );
    const addButton = container.querySelector(
      '[data-testid="ai-provider-add"]',
    ) as HTMLButtonElement | null;

    await act(async () => {
      addButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    await fillInput('[data-testid="ai-provider-name"]', "ctsn");
    await fillInput('[data-testid="ai-provider-base-url"]', "https://api.ctsn.cc/v1");
    await fillInput('[data-testid="ai-provider-api-key"]', "secret");

    const fetchButton = document.body.querySelector(
      '[data-testid="ai-provider-fetch-models"]',
    ) as HTMLButtonElement | null;
    await act(async () => {
      fetchButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    const testButton = document.body.querySelector(
      '[data-testid="ai-provider-test"]',
    ) as HTMLButtonElement | null;
    await act(async () => {
      testButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(apiPostMock).toHaveBeenCalledWith("/settings/ai/provider/test", {
      provider: expect.objectContaining({
        name: "ctsn",
        providerKind: "openai-compatible",
        baseUrl: "https://api.ctsn.cc/v1",
        apiKey: "secret",
        model: "gpt-5.5",
      }),
      prompt: "hello",
    });
  });

  it("keeps the provider modal available in provider-only embedded mode", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded="provider-only" />,
      "/ai-workbench/model-settings",
    );
    const addButton = container.querySelector(
      '[data-testid="ai-provider-add"]',
    ) as HTMLButtonElement | null;

    expect(addButton).not.toBeNull();

    await act(async () => {
      addButton?.click();
      await Promise.resolve();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(
      document.body.querySelector('[data-testid="ai-provider-modal"]'),
    ).not.toBeNull();
    expect(
      document.body.querySelector('[data-testid="ai-provider-form"]'),
    ).not.toBeNull();
  });

  it("offers skywalking as a traces backend option in AI data sources", async () => {
    const source = await import("./settings-pages");

    expect(source).toBeTruthy();
    expect(
      (
        source as unknown as {
          __testOnly?: { tracesBackendOptions?: Array<{ value: string }> };
        }
      ).__testOnly?.tracesBackendOptions?.map((item) => item.value),
    ).toEqual(["jaeger", "skywalking"]);
  });
});
