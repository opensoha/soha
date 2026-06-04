/** @vitest-environment jsdom */

import { act } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRoot } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "antd";
import { LoginPage } from "./login-page";

vi.mock("@/features/auth/auth-api", async () => {
  const actual =
    await vi.importActual<typeof import("@/features/auth/auth-api")>(
      "@/features/auth/auth-api",
    );
  return {
    ...actual,
    fetchAuthProviders: vi.fn(async () => []),
    fetchLoginOptions: vi.fn(async () => ({ verification: { sliderEnabled: false } })),
    fetchPermissionSnapshot: vi.fn(async () => ({
      permissionKeys: [],
      visibleMenuIds: [],
      visibleMenus: [],
    })),
    loginWithPassword: vi.fn(),
  };
});

vi.mock("@/stores/preferences-store", () => ({
  usePreferencesStore: (selector: (state: any) => unknown) =>
    selector({
      currentWorkspace: "resource",
      setThemeMode: vi.fn(),
      themeMode: "light",
    }),
}));

vi.mock("@/utils/branding", () => ({
  readStoredBrandingSettings: () => ({
    appTitle: "Soha",
    collapsedLogoUrl: "",
    expandedLogoUrl: "",
    faviconUrl: "",
    loginLogoUrl: "",
    sidebarTitle: "Soha",
  }),
}));

let containers: HTMLDivElement[] = [];
let roots: Array<ReturnType<typeof createRoot>> = [];

async function renderLoginPage() {
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
      <QueryClientProvider client={queryClient}>
        <App>
          <MemoryRouter initialEntries={["/login"]}>
            <LoginPage />
          </MemoryRouter>
        </App>
      </QueryClientProvider>,
    );
  });

  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });

  return container;
}

describe("login page", () => {
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

    Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
      configurable: true,
      value: vi.fn(() => ({
        arc: vi.fn(),
        beginPath: vi.fn(),
        clearRect: vi.fn(),
        createLinearGradient: vi.fn(() => ({ addColorStop: vi.fn() })),
        fill: vi.fn(),
        fillRect: vi.fn(),
        lineTo: vi.fn(),
        moveTo: vi.fn(),
        restore: vi.fn(),
        save: vi.fn(),
        setTransform: vi.fn(),
        stroke: vi.fn(),
      })),
    });

    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.stubGlobal("ResizeObserver", ResizeObserverMock);
    vi.stubGlobal("requestAnimationFrame", vi.fn(() => 1));
    vi.stubGlobal("cancelAnimationFrame", vi.fn());
  });

  beforeEach(() => {
    document.documentElement.dataset.themeMode = "light";
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

  it("renders copyright on the login page", async () => {
    const container = await renderLoginPage();

    expect(container.querySelector(".soha-auth-copyright")?.textContent).toBe(
      "© 2026 Soha 版权所有，由项目贡献者设计与开发。",
    );
  });
});
