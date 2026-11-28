import { CloseOutlined, LayoutOutlined } from '@ant-design/icons'
import { Button, Tooltip } from 'antd'
import { useEffect, useRef, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { create } from 'zustand'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'

export interface SplitCardDrawerPayload {
  ownerKey?: string
  title: ReactNode
  body: ReactNode
  footer?: ReactNode
  extra?: ReactNode
  width?: number
  onSwitchToFloating?: () => void
  onClose?: () => void
}

interface SplitCardDrawerState {
  open: boolean
  sessionGeneration: number | null
  payload: SplitCardDrawerPayload | null
  openSplitCard: (payload: SplitCardDrawerPayload) => void
  closeSplitCard: () => void
  resetSplitCard: () => void
}

export const useSplitCardDrawerStore = create<SplitCardDrawerState>((set, get) => ({
  open: false,
  sessionGeneration: null,
  payload: null,
  openSplitCard: (payload) =>
    set({
      open: true,
      sessionGeneration: useAuthStore.getState().sessionGeneration,
      payload,
    }),
  closeSplitCard: () => {
    const prev = get().payload
    set({ open: false, sessionGeneration: null, payload: null })
    prev?.onClose?.()
  },
  resetSplitCard: () => {
    if (!get().open && !get().payload) return
    set({ open: false, sessionGeneration: null, payload: null })
  },
}))

/**
 * Inline Right-Side Rounded Card Island Drawer (`卡片式并排圆角岛抽屉`).
 * Expands as a peer sibling card on the right side of `<Content>` without a modal mask
 * or floating overlay — turning any page into a multi-card side-by-side workspace.
 */
export function SplitCardDrawerHost() {
  const { localeCode } = useI18n()
  const { pathname } = useLocation()
  const currentSessionGeneration = useAuthStore((state) => state.sessionGeneration)
  const open = useSplitCardDrawerStore((state) => state.open)
  const drawerSessionGeneration = useSplitCardDrawerStore((state) => state.sessionGeneration)
  const payload = useSplitCardDrawerStore((state) => state.payload)
  const closeSplitCard = useSplitCardDrawerStore((state) => state.closeSplitCard)
  const resetSplitCard = useSplitCardDrawerStore((state) => state.resetSplitCard)
  const lastScopeRef = useRef({ pathname, sessionGeneration: currentSessionGeneration })

  useEffect(() => {
    const prev = lastScopeRef.current
    if (prev.pathname !== pathname || prev.sessionGeneration !== currentSessionGeneration) {
      lastScopeRef.current = { pathname, sessionGeneration: currentSessionGeneration }
      resetSplitCard()
    }
  }, [pathname, currentSessionGeneration, resetSplitCard])

  if (!open || !payload || drawerSessionGeneration !== currentSessionGeneration) return null

  return (
    <aside
      aria-label={localeCode === 'zh_CN' ? '右侧卡片详情面板' : 'Right detail card panel'}
      className="soha-split-card-drawer"
      style={payload.width ? { flexBasis: `${payload.width}px` } : undefined}
    >
      <div className="soha-split-card-drawer__inner">
        <header className="soha-split-card-drawer__header">
          <div className="soha-split-card-drawer__title">{payload.title}</div>
          <div className="soha-split-card-drawer__actions">
            {payload.extra}
            {payload.onSwitchToFloating ? (
              <Tooltip
                title={
                  localeCode === 'zh_CN'
                    ? '切换为悬浮遮罩抽屉'
                    : 'Switch to floating overlay drawer'
                }
              >
                <Button
                  type="text"
                  size="small"
                  className="soha-split-card-drawer__mode-btn"
                  icon={<LayoutOutlined />}
                  aria-label={
                    localeCode === 'zh_CN' ? '切换为悬浮抽屉' : 'Switch to floating drawer'
                  }
                  onClick={() => {
                    const cb = payload.onSwitchToFloating
                    resetSplitCard()
                    cb?.()
                  }}
                />
              </Tooltip>
            ) : null}
            <Button
              type="text"
              size="small"
              className="soha-split-card-drawer__close-btn"
              icon={<CloseOutlined />}
              aria-label={localeCode === 'zh_CN' ? '关闭右侧卡片' : 'Close card drawer'}
              onClick={closeSplitCard}
            />
          </div>
        </header>

        <div className="soha-split-card-drawer__body">{payload.body}</div>

        {payload.footer ? (
          <footer className="soha-split-card-drawer__footer">{payload.footer}</footer>
        ) : null}
      </div>
    </aside>
  )
}
