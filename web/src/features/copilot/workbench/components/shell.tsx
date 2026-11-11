import { useEffect, useRef, useState, type ReactNode } from 'react'
import { CloseOutlined } from '@ant-design/icons'
import { Button, Drawer, Grid, Tooltip } from 'antd'
import { Link, useLocation } from 'react-router-dom'
import { AnimatedAssistantIcon } from './animated-icon'

export function WorkbenchShell({
  personal = false,
  alerts,
  onSearch,
  onChat,
  chatActive,
  children,
  sidebar,
  railFooter,
  panelOpen,
  onClosePanel,
  assistantPanelOpen,
  railActions,
  mobileTab = 'chat',
  onMobileTab,
}: {
  personal?: boolean
  alerts?: ReactNode
  onSearch: () => void
  onChat: () => void
  chatActive: boolean
  children: ReactNode
  sidebar: ReactNode
  railFooter: ReactNode
  panelOpen: boolean
  onClosePanel: () => void
  assistantPanelOpen?: boolean
  railActions?: ReactNode
  mobileTab?: MobileAssistantTab
  onMobileTab?: (tab: MobileAssistantTab) => void
}) {
  const screens = Grid.useBreakpoint()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(false)
  const [sessionsOpen, setSessionsOpen] = useState(false)
  const sidebarVisible = Boolean(
    screens.lg && !collapsed && (!panelOpen || assistantPanelOpen || screens.xxl),
  )
  const toggleSessions = () => {
    if (!screens.lg) {
      onClosePanel()
      setSessionsOpen(!sessionsOpen)
    } else if (panelOpen && !assistantPanelOpen && !screens.xxl) {
      onClosePanel()
      setCollapsed(false)
    } else setCollapsed(!collapsed)
  }

  useEffect(() => setSessionsOpen(false), [location.pathname, location.search])

  return (
    <div className="soha-ai-workbench-page soha-ai-assistant">
      <nav className="soha-ai-icon-rail" aria-label={personal ? 'Heya 导航' : 'AI 工作台对话导航'}>
        <Link className="soha-ai-rail-brand" to="/portal" aria-label="返回门户">
          <img src={personal ? '/heya-avatar.svg' : '/logo.svg'} alt="" />
        </Link>
        <div className="soha-ai-rail-actions">
          <Tooltip title="对话" placement="right">
            <Button
              className="soha-ai-rail-action soha-ai-icon-trigger"
              aria-label={
                !chatActive ? '返回对话' : sidebarVisible ? '收起会话列表' : '展开会话列表'
              }
              aria-expanded={sidebarVisible || sessionsOpen}
              type="text"
              icon={<AnimatedAssistantIcon name="chat" />}
              onClick={chatActive ? toggleSessions : onChat}
            />
          </Tooltip>
          <Tooltip title="搜索对话" placement="right">
            <Button
              className="soha-ai-rail-action soha-ai-icon-trigger"
              type="text"
              aria-label="搜索对话"
              icon={<AnimatedAssistantIcon name="search" />}
              onClick={onSearch}
            />
          </Tooltip>
          {railActions}
        </div>
        <div className="soha-ai-rail-footer" role="group" aria-label="助手设置与工作台">
          {railFooter}
          {personal ? (
            <Tooltip title="Heya 开通状态" placement="right">
              <Link className="soha-ai-rail-action" to="/heya/access" aria-label="Heya 开通状态">
                <AnimatedAssistantIcon name="identity" />
              </Link>
            </Tooltip>
          ) : null}
          <Tooltip title="AI 工作台" placement="right">
            <Link
              className="soha-ai-rail-action soha-ai-icon-trigger"
              to="/ai-workbench/overview"
              aria-label="AI 工作台"
            >
              <AnimatedAssistantIcon name="workbench" />
            </Link>
          </Tooltip>
        </div>
      </nav>
      <div className="soha-ai-workbench">
        {alerts}
        <section
          className={`soha-ai-workbench__workspace${sidebarVisible ? ' has-sidebar' : ''}${panelOpen && screens.lg ? ' has-detail' : ''}${assistantPanelOpen ? ' has-assistant' : ''}`}
        >
          {!screens.lg ? (
            <Button
              className="soha-ai-topic-toggle soha-ai-icon-trigger"
              aria-label="展开话题"
              type="text"
              icon={<AnimatedAssistantIcon name="chat" />}
              onClick={toggleSessions}
            />
          ) : null}
          <div className="soha-ai-workbench__session-slot" hidden={!sidebarVisible}>
            {screens.lg ? sidebar : null}
          </div>
          {children}
        </section>
      </div>
      {!screens.lg ? (
        <Drawer
          rootClassName="soha-ai-assistant"
          title="会话记录"
          placement="left"
          size="min(320px, 100vw)"
          open={sessionsOpen}
          onClose={() => setSessionsOpen(false)}
        >
          <div className="soha-ai-workbench__session-slot">{sidebar}</div>
        </Drawer>
      ) : null}
      {!screens.lg && onMobileTab ? (
        <MobileAssistantNav value={mobileTab} onChange={onMobileTab} />
      ) : null}
    </div>
  )
}

export type MobileAssistantTab = 'chat' | 'feed' | 'tasks' | 'results' | 'me'

export function MobileAssistantNav({
  value,
  onChange,
}: {
  value: MobileAssistantTab
  onChange: (tab: MobileAssistantTab) => void
}) {
  return (
    <nav className="soha-ai-mobile-nav" aria-label="个人助手">
      {[
        { key: 'chat' as const, label: '对话', icon: <AnimatedAssistantIcon name="chat" /> },
        { key: 'feed' as const, label: '动态', icon: <AnimatedAssistantIcon name="tasks" /> },
        { key: 'tasks' as const, label: '任务', icon: <AnimatedAssistantIcon name="tasks" /> },
        { key: 'results' as const, label: '成果', icon: <AnimatedAssistantIcon name="results" /> },
        { key: 'me' as const, label: '我的', icon: <AnimatedAssistantIcon name="identity" /> },
      ].map((item) => (
        <Button
          key={item.key}
          className="soha-ai-icon-trigger"
          type="text"
          aria-label={item.label}
          aria-current={value === item.key ? 'page' : undefined}
          icon={item.icon}
          onClick={() => onChange(item.key)}
        >
          {item.label}
        </Button>
      ))}
    </nav>
  )
}

export function WorkbenchPanel({
  title,
  open,
  onClose,
  onAfterClose,
  extra,
  footer,
  children,
  variant,
}: {
  title: string
  open: boolean
  onClose: () => void
  onAfterClose?: () => void
  extra?: ReactNode
  footer?: ReactNode
  children: ReactNode
  variant?: 'assistant'
}) {
  const screens = Grid.useBreakpoint()
  const headingRef = useRef<HTMLHeadingElement>(null)
  const triggerRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!open || !screens.lg) return
    triggerRef.current = document.activeElement as HTMLElement | null
    headingRef.current?.focus()
  }, [open, screens.lg])

  const closePanel = () => {
    if (triggerRef.current?.isConnected) triggerRef.current.focus()
    onClose()
  }

  if (!screens.lg) {
    return (
      <Drawer
        title={variant === 'assistant' ? <span className="soha-ai-sr-only">{title}</span> : title}
        open={open}
        onClose={onClose}
        afterOpenChange={(visible) => {
          if (!visible) onAfterClose?.()
        }}
        size="100%"
        closable={variant === 'assistant' ? { placement: 'end' } : true}
        extra={extra}
        footer={footer}
        rootClassName={`soha-ai-workbench__mobile-detail${variant === 'assistant' ? ' is-assistant' : ''}`}
      >
        {children}
      </Drawer>
    )
  }
  if (!open) return null
  return (
    <aside
      className={`soha-ai-workbench__detail${variant === 'assistant' ? ' is-assistant' : ''}`}
      aria-label={title}
      onKeyDown={(event) => {
        if (event.key === 'Escape' && event.currentTarget.contains(event.target as Node)) {
          event.stopPropagation()
          closePanel()
        }
      }}
    >
      <div className="soha-ai-workbench__detail-header">
        <div className="soha-ai-workbench__detail-title">
          <h2
            ref={headingRef}
            tabIndex={-1}
            className={variant === 'assistant' ? 'soha-ai-sr-only' : undefined}
          >
            {title}
          </h2>
          <Button
            type="text"
            aria-label={`关闭${title}`}
            icon={<CloseOutlined />}
            onClick={closePanel}
          />
        </div>
        {extra}
      </div>
      <div className="soha-ai-workbench__detail-body">{children}</div>
      {footer ? <div className="soha-ai-workbench__detail-footer">{footer}</div> : null}
    </aside>
  )
}
