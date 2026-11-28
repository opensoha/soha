import {
  AppstoreOutlined,
  CloudServerOutlined,
  ClusterOutlined,
  DeploymentUnitOutlined,
  RadarChartOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { Button, Space, Tag, Typography } from 'antd'
import { useNavigate } from 'react-router-dom'
import './about-page.css'

const { Paragraph, Text, Title } = Typography

const CAPABILITY_PILLARS = [
  {
    key: 'platform',
    domain: 'Kubernetes',
    title: '容器与集群控制面',
    desc: '多集群统一纳管、工作负载编排、实时容器终端日志与服务网络拓扑排障。',
    icon: <ClusterOutlined />,
  },
  {
    key: 'compute',
    domain: 'Compute & VM',
    title: '异构计算与虚拟化',
    desc: '打通 Proxmox VE、KubeVirt 虚拟机与容器运行时主机，统一镜像、规格与任务调度。',
    icon: <CloudServerOutlined />,
  },
  {
    key: 'delivery',
    domain: 'Continuous Delivery',
    title: '声明式应用交付',
    desc: '面向多环境发布看板、金丝雀批次推进、变更审批门禁与全流程构建执行回溯。',
    icon: <DeploymentUnitOutlined />,
  },
  {
    key: 'observability',
    domain: 'Observability',
    title: '全栈可观测与告警',
    desc: '联动指标、日志、分布式链路追踪与告警收敛通道，快速定位线上异常根因。',
    icon: <RadarChartOutlined />,
  },
  {
    key: 'ai',
    domain: 'Heya AI Gateway',
    title: '智能副驾与模型网关',
    desc: '受控 MCP 工具调用、高风险操作人工确认、个人访问凭据轮换与多模型路由治理。',
    icon: <RobotOutlined />,
  },
  {
    key: 'portal',
    domain: 'Zero-Trust Portal',
    title: '统一身份与应用门户',
    desc: '提供 OIDC、SAML 与反向代理单点登录入口，结合多因素认证与细粒度访问审计。',
    icon: <SafetyCertificateOutlined />,
  },
] as const

export function AboutPage() {
  const navigate = useNavigate()

  return (
    <div className="soha-page soha-about-page">
      <div className="soha-about-hero-bezel">
        <section className="soha-about-hero">
          <div className="soha-about-hero__main">
            <div className="soha-about-brand-row">
              <span className="soha-about-monogram" aria-hidden="true">
                S
              </span>
              <div className="soha-about-brand-copy">
                <Title level={3}>关于 Soha</Title>
                <p className="soha-about-tagline">Soha，解决中小企业运维难题。</p>
              </div>
            </div>
            <Paragraph className="soha-about-lead">
              让人手有限的团队，更轻松地管理、维护和保障业务运行。围绕实际运维需要持续接入新能力。
            </Paragraph>
            <div className="soha-about-badges">
              <Tag color="blue">运维工作台</Tag>
              <Tag>Apache-2.0</Tag>
              <Tag>Self-Hosted</Tag>
              <Tag>OpenSoha Core</Tag>
            </div>
          </div>

          <div className="soha-about-spec-grid" aria-label="系统架构信息">
            <div className="soha-about-spec-tile">
              <span className="soha-about-spec-tile__label">开源许可</span>
              <span className="soha-about-spec-tile__value">Apache-2.0 License</span>
            </div>
            <div className="soha-about-spec-tile">
              <span className="soha-about-spec-tile__label">部署边界</span>
              <span className="soha-about-spec-tile__value">开源核心 · 独立自托管</span>
            </div>
            <div className="soha-about-spec-tile">
              <span className="soha-about-spec-tile__label">契约基座</span>
              <span className="soha-about-spec-tile__value">OpenAPI · CLI · Agent</span>
            </div>
            <div className="soha-about-spec-tile">
              <span className="soha-about-spec-tile__label">权限模型</span>
              <span className="soha-about-spec-tile__value">RBAC 快照 · 审计留痕</span>
            </div>
          </div>
        </section>
      </div>

      <div className="soha-about-section-head">
        <span className="soha-about-section-title">核心能力矩阵</span>
        <Text type="secondary">统一控制面 · 模块化工作台</Text>
      </div>

      <section className="soha-about-pillars" aria-label="核心能力模块">
        {CAPABILITY_PILLARS.map((item) => (
          <article className="soha-about-pillar-card" key={item.key}>
            <div className="soha-about-pillar-card__top">
              <span className="soha-about-pillar-icon">{item.icon}</span>
              <span className="soha-about-pillar-domain">{item.domain}</span>
            </div>
            <div className="soha-about-pillar-title">{item.title}</div>
            <div className="soha-about-pillar-desc">{item.desc}</div>
          </article>
        ))}
      </section>

      <footer className="soha-about-governance">
        <div className="soha-about-governance__meta">
          <span className="soha-about-governance__dot" aria-hidden="true" />
          <Text strong>OpenSoha</Text>
          <Text type="secondary">Soha 由 OpenSoha 社区维护，开源、自托管。</Text>
        </div>
        <Space size={8} wrap>
          <Button icon={<UserOutlined />} size="small" onClick={() => navigate('/account/profile')}>
            个人中心
          </Button>
          <Button
            icon={<AppstoreOutlined />}
            size="small"
            type="primary"
            onClick={() => navigate('/portal')}
          >
            门户首页
          </Button>
        </Space>
      </footer>
    </div>
  )
}
