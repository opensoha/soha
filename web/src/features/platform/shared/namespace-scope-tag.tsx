import { useEffect } from 'react'
import { useDeferredOverlay } from '@/components/use-deferred-overlay'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { Tooltip } from 'antd'
import { useSearchParams } from 'react-router-dom'
import './namespace-scope-tag.css'

export function NamespaceScopeTag({ namespace }: { namespace?: string | null }) {
  const { localeCode } = useI18n()
  const { clusterId, setNamespace } = usePlatformScopeStore()
  const [, setSearchParams] = useSearchParams()
  const value = namespace?.trim()
  const { triggerRef, activated, open, setOpen, activate } = useDeferredOverlay<HTMLButtonElement>()
  useEffect(() => {
    if (!value || value === '-') setOpen(false)
  }, [value, setOpen])
  if (!value || value === '-') return <span>—</span>
  const trigger = (
    <button
      ref={triggerRef}
      onMouseEnter={!activated ? activate : undefined}
      type="button"
      className="soha-namespace-scope-button"
      aria-label={
        localeCode === 'zh_CN' ? `切换到命名空间 ${value}` : `Switch to namespace ${value}`
      }
      onClick={(event) => {
        event.stopPropagation()
        setNamespace(value)
        setSearchParams(
          (previous) => {
            const next = new URLSearchParams(previous)
            next.set('namespace', value)
            if (clusterId) next.set('clusterId', clusterId)
            return next
          },
          { replace: true },
        )
      }}
    >
      <MetadataTag
        tone="blue"
        className="soha-namespace-scope-tag"
        label={<span className="soha-namespace-scope-label">{value}</span>}
      />
    </button>
  )
  return activated ? (
    <Tooltip
      title={value}
      placement="top"
      styles={{ container: { overflowWrap: 'anywhere' } }}
      open={open}
      onOpenChange={setOpen}
    >
      {trigger}
    </Tooltip>
  ) : (
    trigger
  )
}
