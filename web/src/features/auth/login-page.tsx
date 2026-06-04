import { useEffect, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  App,
  Button,
  Card,
  Divider,
  Form,
  Input,
  Slider,
  Typography,
} from "antd";
import {
  CheckCircleOutlined,
  LockOutlined,
  MoonOutlined,
  SafetyCertificateOutlined,
  SunOutlined,
  UserOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import {
  commitAuthResult,
  fetchAuthProviders,
  fetchLoginOptions,
  fetchPermissionSnapshot,
  loginWithPassword,
} from "@/features/auth/auth-api";
import { findLandingPath } from "@/routes/meta";
import { usePreferencesStore } from "@/stores/preferences-store";
import { resolveThemeMode } from "@/theme/app-theme";
import { readStoredBrandingSettings } from "@/utils/branding";
import "./login-page.css";

const { Title, Text } = Typography;

const LOGIN_COPYRIGHT_TEXT = "© 2026 Soha 版权所有，由项目贡献者设计与开发。";

interface LoginFormValues {
  password: string;
  username: string;
}

interface StarNode {
  pulse: number;
  radius: number;
  tone: string;
  vx: number;
  vy: number;
  x: number;
  y: number;
}

const STAR_TONES = [
  "56, 189, 248",
  "45, 212, 191",
  "34, 197, 94",
  "245, 158, 11",
];

const CAPABILITY_QUESTIONS = [
  {
    question: "Soha 支持哪些工作台？",
    answer: "K8s、虚拟化、Docker、监控、AI 与系统治理统一入口",
  },
  {
    question: "虚拟机和容器怎么一起管？",
    answer: "KubeVirt / PVE 虚拟机、Docker 主机与 Compose 项目联动",
  },
  {
    question: "AI 工作台能做什么？",
    answer: "会话、根因、巡检、性能分析进入同一排障上下文",
  },
  {
    question: "AI Gateway 怎么接入？",
    answer: "模型供应商、工具绑定、策略与审计统一治理",
  },
  {
    question: "交付和运行环境能闭环吗？",
    answer: "应用、环境、发布包、执行任务和回调证据持续串联",
  },
  {
    question: "告警、值班和权限如何协同？",
    answer: "监控事件、OnCall 路由、RBAC、菜单和审计同源",
  },
];

function createStarNode(width: number, height: number): StarNode {
  const angle = Math.random() * Math.PI * 2;
  const speed = 0.05 + Math.random() * 0.14;

  return {
    pulse: Math.random() * Math.PI * 2,
    radius: 1 + Math.random() * 1.8,
    tone: STAR_TONES[Math.floor(Math.random() * STAR_TONES.length)]!,
    vx: Math.cos(angle) * speed,
    vy: Math.sin(angle) * speed,
    x: Math.random() * width,
    y: Math.random() * height,
  };
}

function LoginConstellationBackground() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    const context = canvas?.getContext("2d");

    if (!canvas || !context) {
      return undefined;
    }

    let animationFrame = 0;
    let width = 1;
    let height = 1;
    let lastTimestamp = 0;
    let nodes: StarNode[] = [];
    let currentThemeMode: "light" | "dark" = "light";
    const reducedMotion = window.matchMedia(
      "(prefers-reduced-motion: reduce)",
    ).matches;
    const readThemeMode = () => {
      currentThemeMode =
        document.documentElement.dataset.themeMode === "dark"
          ? "dark"
          : "light";
    };

    const resize = () => {
      const rect = canvas.getBoundingClientRect();
      width = Math.max(1, rect.width);
      height = Math.max(1, rect.height);
      const ratio = Math.min(window.devicePixelRatio || 1, 2);

      canvas.width = Math.floor(width * ratio);
      canvas.height = Math.floor(height * ratio);
      context.setTransform(ratio, 0, 0, ratio, 0, 0);

      const nextCount = Math.round(
        Math.min(118, Math.max(58, (width * height) / 15000)),
      );

      if (nodes.length > nextCount) {
        nodes = nodes.slice(0, nextCount);
      }
      while (nodes.length < nextCount) {
        nodes.push(createStarNode(width, height));
      }
      nodes = nodes.map((node) =>
        node.x > width || node.y > height
          ? createStarNode(width, height)
          : node,
      );
    };

    const draw = (timestamp: number) => {
      readThemeMode();
      const delta = lastTimestamp
        ? Math.min(32, timestamp - lastTimestamp) / 16.67
        : 1;
      lastTimestamp = timestamp;

      const gradient = context.createLinearGradient(0, 0, width, height);
      if (currentThemeMode === "dark") {
        gradient.addColorStop(0, "#0f172a");
        gradient.addColorStop(0.52, "#111827");
        gradient.addColorStop(1, "#0b1d24");
      } else {
        gradient.addColorStop(0, "#f8fafc");
        gradient.addColorStop(0.48, "#eef2f7");
        gradient.addColorStop(1, "#e7edf4");
      }
      context.fillStyle = gradient;
      context.fillRect(0, 0, width, height);

      context.save();
      context.strokeStyle =
        currentThemeMode === "dark"
          ? "rgba(148, 163, 184, 0.06)"
          : "rgba(100, 116, 139, 0.085)";
      context.lineWidth = 1;
      for (let x = 0; x <= width; x += 72) {
        context.beginPath();
        context.moveTo(x, 0);
        context.lineTo(x, height);
        context.stroke();
      }
      for (let y = 0; y <= height; y += 72) {
        context.beginPath();
        context.moveTo(0, y);
        context.lineTo(width, y);
        context.stroke();
      }
      context.restore();

      const connectionDistance = Math.max(116, Math.min(168, width / 7));

      nodes.forEach((node) => {
        if (!reducedMotion) {
          node.x += node.vx * delta;
          node.y += node.vy * delta;
          node.pulse += 0.016 * delta;

          if (node.x < -24) node.x = width + 24;
          if (node.x > width + 24) node.x = -24;
          if (node.y < -24) node.y = height + 24;
          if (node.y > height + 24) node.y = -24;
        }
      });

      for (let index = 0; index < nodes.length; index += 1) {
        const source = nodes[index]!;
        for (let nextIndex = index + 1; nextIndex < nodes.length; nextIndex += 1) {
          const target = nodes[nextIndex]!;
          const dx = source.x - target.x;
          const dy = source.y - target.y;
          const distance = Math.hypot(dx, dy);

          if (distance > connectionDistance) {
            continue;
          }

          const alpha =
            (1 - distance / connectionDistance) *
            (currentThemeMode === "dark" ? 0.32 : 0.2);
          context.strokeStyle = `rgba(${source.tone}, ${alpha})`;
          context.lineWidth = 1;
          context.beginPath();
          context.moveTo(source.x, source.y);
          context.lineTo(target.x, target.y);
          context.stroke();
        }
      }

      nodes.forEach((node) => {
        const pulse = 0.28 + Math.sin(node.pulse) * 0.12;
        context.fillStyle = `rgba(${node.tone}, ${
          currentThemeMode === "dark" ? 0.54 + pulse : 0.42 + pulse * 0.62
        })`;
        context.beginPath();
        context.arc(node.x, node.y, node.radius, 0, Math.PI * 2);
        context.fill();

        context.fillStyle = `rgba(${node.tone}, ${
          currentThemeMode === "dark" ? 0.06 + pulse * 0.12 : 0.035 + pulse * 0.08
        })`;
        context.beginPath();
        context.arc(node.x, node.y, node.radius * 6.5, 0, Math.PI * 2);
        context.fill();
      });

      if (!reducedMotion) {
        animationFrame = window.requestAnimationFrame(draw);
      }
    };

    const resizeObserver = new ResizeObserver(() => {
      resize();
      if (reducedMotion) {
        draw(performance.now());
      }
    });

    resizeObserver.observe(canvas);
    const themeObserver = new MutationObserver(() => {
      if (reducedMotion) {
        draw(performance.now());
      }
    });
    themeObserver.observe(document.documentElement, {
      attributeFilter: ["data-theme-mode"],
      attributes: true,
    });
    resize();
    draw(performance.now());

    return () => {
      resizeObserver.disconnect();
      themeObserver.disconnect();
      window.cancelAnimationFrame(animationFrame);
    };
  }, []);

  return (
    <canvas
      ref={canvasRef}
      className="soha-auth-background"
      aria-hidden="true"
    />
  );
}

function LoginCapabilityFlow() {
  return (
    <section className="soha-auth-flow" aria-label="Soha 支持的能力">
      <div className="soha-auth-flow-copy">
        <div>
          <Title level={1} className="soha-auth-flow-title">
            Soha 让工作快乐起来！
          </Title>
          <Text className="soha-auth-flow-subtitle">
            从 K8s、多集群、虚拟机、Docker 到 AI Gateway、AI 工作台与监控值班，让关键工作在同一条协作链路上自然衔接。
          </Text>
        </div>
      </div>

      <div className="soha-auth-flow-map">
        <svg
          className="soha-auth-flow-lines"
          viewBox="0 0 640 360"
          preserveAspectRatio="none"
          aria-hidden="true"
        >
          <defs>
            <linearGradient id="soha-auth-flow-gradient" x1="0" x2="1" y1="0" y2="0">
              <stop offset="0%" stopColor="#94a3b8" stopOpacity="0.18" />
              <stop offset="46%" stopColor="#38bdf8" stopOpacity="0.72" />
              <stop offset="68%" stopColor="#2dd4bf" stopOpacity="0.78" />
              <stop offset="100%" stopColor="#94a3b8" stopOpacity="0.18" />
            </linearGradient>
          </defs>
          <path className="soha-auth-flow-line" d="M80 72 C210 24 250 118 332 116" />
          <path className="soha-auth-flow-line is-delay-1" d="M332 116 C444 114 472 58 562 90" />
          <path className="soha-auth-flow-line is-delay-2" d="M332 116 C278 152 178 142 118 206" />
          <path className="soha-auth-flow-line is-delay-3" d="M118 206 C238 248 332 214 452 254" />
          <path className="soha-auth-flow-line is-delay-1" d="M562 90 C590 156 548 212 452 254" />
          <path className="soha-auth-flow-line is-delay-2" d="M452 254 C384 330 296 338 210 330" />
          <path className="soha-auth-flow-line is-muted" d="M80 72 C120 150 86 190 118 206" />
        </svg>

        {CAPABILITY_QUESTIONS.map((item, index) => (
          <article
            key={item.question}
            className={`soha-auth-flow-node soha-auth-flow-node--${index}`}
          >
            <span className="soha-auth-flow-node__dot" />
            <div>
              <div className="soha-auth-flow-node__question">{item.question}</div>
              <div className="soha-auth-flow-node__answer">{item.answer}</div>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function getProviderLabel(type: string, name: string) {
  if (type === "oidc") {
    return name || "OIDC";
  }
  if (type === "feishu") return name || "飞书";
  if (type === "dingtalk") return name || "钉钉";
  if (type === "wecom") return name || "企业微信";
  if (type === "saml") return name || "SAML";
  return name || type.toUpperCase();
}

function getProviderIcon(type: string) {
  if (type === "oidc" || type === "saml") {
    return <SafetyCertificateOutlined />;
  }
  return <UserOutlined />;
}

function normalizeSliderValue(value: number | number[]) {
  return Array.isArray(value) ? value[0] ?? 0 : value;
}

export function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { message } = App.useApp();
  const [loading, setLoading] = useState(false);
  const [providerLoadingKey, setProviderLoadingKey] = useState<string | null>(
    null,
  );
  const [sliderValue, setSliderValue] = useState(0);
  const [sliderVerified, setSliderVerified] = useState(false);
  const themeMode = usePreferencesStore((state) => state.themeMode);
  const setThemeMode = usePreferencesStore((state) => state.setThemeMode);

  const branding = readStoredBrandingSettings();
  const from = (location.state as { from?: { pathname: string } } | undefined)
    ?.from?.pathname;
  const providersQuery = useQuery({
    queryKey: ["auth-providers"],
    queryFn: fetchAuthProviders,
    staleTime: 60_000,
  });
  const loginOptionsQuery = useQuery({
    queryKey: ["auth-login-options"],
    queryFn: fetchLoginOptions,
    retry: false,
    staleTime: 60_000,
  });
  const thirdPartyProviders = (providersQuery.data ?? []).filter(
    (item) => item.enabled !== false && item.type !== "password",
  );
  const appTitle = branding.sidebarTitle || branding.appTitle || "Soha";
  const resolvedThemeMode = resolveThemeMode(themeMode);
  const sliderVerificationEnabled =
    loginOptionsQuery.data?.verification?.sliderEnabled === true;
  const toggleThemeMode = () => {
    setThemeMode(resolvedThemeMode === "dark" ? "light" : "dark");
  };
  const resolvePostLoginPath = async (
    roles: string[],
    explicitPath?: string,
  ) => {
    if (explicitPath && explicitPath !== "/login") {
      return explicitPath;
    }
    try {
      const snapshot = await fetchPermissionSnapshot();
      return (
        findLandingPath(
          snapshot,
          usePreferencesStore.getState().currentWorkspace,
          roles,
        ) ?? "/"
      );
    } catch {
      return "/";
    }
  };

  const resetSliderVerification = () => {
    setSliderValue(0);
    setSliderVerified(false);
  };

  const handleSliderChange = (value: number | number[]) => {
    const nextValue = normalizeSliderValue(value);
    setSliderValue(nextValue);
    if (nextValue < 98 && sliderVerified) {
      setSliderVerified(false);
    }
  };

  const handleSliderComplete = (value: number | number[]) => {
    const nextValue = normalizeSliderValue(value);
    if (nextValue >= 98) {
      setSliderValue(100);
      setSliderVerified(true);
      return;
    }
    resetSliderVerification();
  };

  const handleLogin = async (values: LoginFormValues) => {
    if (sliderVerificationEnabled && !sliderVerified) {
      message.warning("请先完成滑块验证");
      return;
    }

    setLoading(true);
    try {
      const authResult = await loginWithPassword(
        values.username,
        values.password,
      );
      commitAuthResult(authResult);
      const nextPath = await resolvePostLoginPath(authResult.user.roles, from);
      message.success("登录成功");
      navigate(nextPath, { replace: true });
    } catch (err: any) {
      if (sliderVerificationEnabled) {
        resetSliderVerification();
      }
      message.error(err?.message ?? "登录失败");
    } finally {
      setLoading(false);
    }
  };

  const handleProviderLogin = (provider: {
    type: string;
    loginUrl?: string;
    name: string;
  }) => {
    if (provider.type === "saml" || !provider.loginUrl) {
      message.warning("当前 SAML 登录配置已保存，但服务端回调链路尚未启用。");
      return;
    }
    setProviderLoadingKey(provider.loginUrl);
    window.location.href = `/api/v1${provider.loginUrl}`;
  };

  return (
    <div className="soha-auth-shell">
      <LoginConstellationBackground />
      <Button
        type="text"
        className="soha-auth-theme-toggle"
        icon={resolvedThemeMode === "dark" ? <SunOutlined /> : <MoonOutlined />}
        aria-label={resolvedThemeMode === "dark" ? "切换浅色模式" : "切换深色模式"}
        onClick={toggleThemeMode}
      />

      <div className="soha-auth-layout soha-auth-layout--floating">
        <LoginCapabilityFlow />

        <Card
          className="soha-auth-panel soha-auth-panel--floating"
          variant="borderless"
        >
          <div className="soha-auth-panel-inner">
            <div className="soha-auth-brand">
              {branding.expandedLogoUrl ? (
                <img
                  src={branding.expandedLogoUrl}
                  alt={branding.sidebarTitle || "Logo"}
                  className="soha-auth-brand-logo-img"
                />
              ) : (
                <div className="soha-auth-mark">SOHA</div>
              )}
              <div className="soha-auth-brand-copy">
                <Title level={4} style={{ margin: 0 }}>
                  {appTitle}
                </Title>
                <Text type="secondary">多工作台统一控制台</Text>
              </div>
            </div>

            <div className="soha-auth-panel-copy">
              <Title level={5} style={{ marginTop: 0, marginBottom: 4 }}>
                登录控制台
              </Title>
            </div>

            <Form<LoginFormValues> layout="vertical" onFinish={handleLogin}>
              <Form.Item<LoginFormValues>
                name="username"
                label="用户名"
                rules={[{ required: true, message: "请输入用户名" }]}
              >
                <Input
                  prefix={<UserOutlined />}
                  placeholder="请输入用户名"
                  allowClear
                  size="middle"
                />
              </Form.Item>
              <Form.Item<LoginFormValues>
                name="password"
                label="密码"
                rules={[{ required: true, message: "请输入密码" }]}
              >
                <Input.Password
                  prefix={<LockOutlined />}
                  placeholder="请输入密码"
                  size="middle"
                />
              </Form.Item>

              {sliderVerificationEnabled ? (
                <Form.Item>
                  <div
                    className={`soha-auth-slider ${
                      sliderVerified ? "is-verified" : ""
                    }`}
                  >
                    <div className="soha-auth-slider-label">
                      <span className="soha-auth-slider-label__icon">
                        {sliderVerified ? (
                          <CheckCircleOutlined />
                        ) : (
                          <SafetyCertificateOutlined />
                        )}
                      </span>
                      <span>
                        {sliderVerified ? "验证通过" : "拖动滑块完成验证"}
                      </span>
                    </div>
                    <div className="soha-auth-slider-track-shell">
                      <span className="soha-auth-slider-shine" />
                      <span className="soha-auth-slider-track-copy">
                        {sliderVerified ? "身份环境检查完成" : "按住滑块向右拖动"}
                      </span>
                    </div>
                    <Slider
                      className="soha-auth-slider-control"
                      disabled={sliderVerified || loading}
                      max={100}
                      min={0}
                      onChange={handleSliderChange}
                      onChangeComplete={handleSliderComplete}
                      step={1}
                      tooltip={{ formatter: null }}
                      value={sliderValue}
                    />
                    <div className="soha-auth-slider-footer">
                      <span>{sliderVerified ? "可提交登录" : "滑到最右侧后解锁登录"}</span>
                      <span>{sliderVerified ? "100%" : `${Math.round(sliderValue)}%`}</span>
                    </div>
                  </div>
                </Form.Item>
              ) : null}

              <Button
                type="primary"
                htmlType="submit"
                loading={loading}
                block
                disabled={sliderVerificationEnabled && !sliderVerified}
                className="soha-auth-submit"
              >
                登录控制台
              </Button>
            </Form>

            {thirdPartyProviders.length > 0 ? (
              <div className="soha-auth-provider-slot">
                <Divider style={{ marginBlock: 18 }}>
                  <Text type="secondary">第三方登录</Text>
                </Divider>

                <div className="soha-auth-provider-list">
                  {thirdPartyProviders.map((provider) => (
                    <Button
                      key={`${provider.type}-${provider.name}`}
                      block
                      loading={providerLoadingKey === provider.loginUrl}
                      onClick={() => handleProviderLogin(provider)}
                      className="soha-auth-provider-button"
                    >
                      <span className="soha-auth-provider-button__content">
                        <span className="soha-auth-provider-button__icon">
                          {provider.type === "saml" && !provider.loginUrl ? (
                            <WarningOutlined />
                          ) : (
                            getProviderIcon(provider.type)
                          )}
                        </span>
                        <span className="soha-auth-provider-button__label">
                          使用 {getProviderLabel(provider.type, provider.name)} 登录
                        </span>
                      </span>
                    </Button>
                  ))}
                </div>
              </div>
            ) : null}
          </div>
        </Card>
      </div>
      <div className="soha-auth-copyright">{LOGIN_COPYRIGHT_TEXT}</div>
    </div>
  );
}
