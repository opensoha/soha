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
}));

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
    get: vi.fn((path: string) =>
      Promise.resolve({ data: testState.responses[path] ?? {} }),
    ),
    put: vi.fn(),
    post: vi.fn(),
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
  });

  beforeEach(() => {
    setDefaultResponses();
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

  it("renders full AI settings content under ai-workbench model settings", async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      "/ai-workbench/model-settings",
    );

    expect(container.textContent).toContain("Provider Connections");
    expect(container.textContent).toContain("Skills Registry");
    expect(container.textContent).toContain("Data Sources");
    expect(
      container.querySelector(
        '[data-testid="ai-provider-connections-section"]',
      ),
    ).not.toBeNull();
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
