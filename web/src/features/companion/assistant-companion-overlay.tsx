import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import type { AIGlobalAssistantAction, AIGlobalAssistantMessage } from '@/features/copilot'
import { DraggableFloatShell } from '@/features/copilot'
import type { InstalledPlugin } from '@/features/plugins'
import { pluginQueries } from '@/features/plugins'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  ExclamationCircleOutlined,
  ExpandAltOutlined,
  HeartOutlined,
  MessageOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Popover } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { companionApi, companionKeys } from './api'
import { BUILTIN_COMPANION_PLUGIN_ID, builtinCompanionPack } from './builtin-pack'
import { CompanionRenderer } from './companion-renderer'
import { useLocalCompanionPackStore } from './local-live2d-store'
import {
  companionInteractionAnimation,
  companionInteractionId,
  type CompanionInteractionMotion,
} from './motion'
import { companionSpeech, resolveCompanionVisualState } from './state-machine'
import type { CompanionPackSelection } from './types'

interface AssistantCompanionOverlayProps {
  disabled?: boolean
  messages: AIGlobalAssistantMessage[]
  panelOpen?: boolean
  running?: boolean
  onAction: (action: AIGlobalAssistantAction) => void
  onOpenAssistant: () => void
  onOpenWorkbench: () => void
  nativeWindow?: boolean
}

function activePack(
  selectedPluginId: string,
  installed: InstalledPlugin[],
  localPack: CompanionPackSelection | null,
): CompanionPackSelection {
  if (localPack) return localPack
  if (selectedPluginId === BUILTIN_COMPANION_PLUGIN_ID) return builtinCompanionPack
  const plugin = installed?.find(
    (item) =>
      item.id === selectedPluginId &&
      item.status === 'enabled' &&
      item.type === 'companion-pack' &&
      item.manifest.companionPack,
  )
  if (!plugin?.manifest.companionPack) return builtinCompanionPack
  return {
    pluginId: plugin.id,
    version: plugin.activeVersion || plugin.version,
    manifest: plugin.manifest.companionPack,
    installed: plugin,
  }
}

export function AssistantCompanionOverlay({
  disabled = false,
  messages,
  panelOpen = false,
  running = false,
  onAction,
  onOpenAssistant,
  onOpenWorkbench,
  nativeWindow = false,
}: AssistantCompanionOverlayProps) {
  const queryClient = useQueryClient()
  const owner = useAuthStore((state) => state.user?.userId ?? '')
  const ownerRef = useRef(owner)
  ownerRef.current = owner
  const selectedPluginId = usePreferencesStore((state) => state.selectedCompanionPluginId)
  const bubbleEnabled = usePreferencesStore((state) => state.companionBubbleEnabled)
  const localPack = useLocalCompanionPackStore((state) => state.pack)
  const [dragging, setDragging] = useState(false)
  const [hovered, setHovered] = useState(false)
  const [interactionMotion, setInteractionMotion] = useState<CompanionInteractionMotion>()
  const interactionTimerRef = useRef<number>()
  const installedQuery = useQuery({ ...pluginQueries.installed(), enabled: !disabled })
  const profileQuery = useQuery({
    queryKey: companionKeys.profile(owner),
    queryFn: companionApi.profile,
    enabled: !disabled && Boolean(owner),
  })
  const allowedRef = useRef(!disabled)
  allowedRef.current = !disabled
  const installed = disabled ? undefined : readableQueryData(installedQuery)
  const pack = useMemo(
    () => activePack(selectedPluginId, Array.isArray(installed) ? installed : [], localPack),
    [installed, localPack, selectedPluginId],
  )
  const interactionMutation = useOperationMutation(
    {
      mutationFn: ({
        input,
      }: {
        owner: string
        input: Parameters<typeof companionApi.interact>[0]
      }) => companionApi.interact(input),
      onSuccess: (receipt, submitted) => {
        if (!allowedRef.current || ownerRef.current !== submitted.owner) return
        queryClient.setQueryData(companionKeys.profile(owner), receipt.profile)
        try {
          const channel = new BroadcastChannel('soha-companion')
          channel.postMessage({ type: 'profile-updated' })
          channel.close()
        } catch {
          // BroadcastChannel is optional; React Query remains authoritative in this view.
        }
      },
    },
    { label: ['宠物互动', 'Companion interaction'], target: (v) => v.input.interactionId },
  )

  useEffect(() => {
    if (typeof BroadcastChannel === 'undefined') return undefined
    const channel = new BroadcastChannel('soha-companion')
    channel.onmessage = () => {
      void queryClient.invalidateQueries({ queryKey: companionKeys.profile(owner) })
    }
    return () => channel.close()
  }, [queryClient, owner])

  useEffect(
    () => () => {
      window.clearTimeout(interactionTimerRef.current)
    },
    [],
  )

  const visualState = resolveCompanionVisualState({
    disabled,
    dragging,
    hovered,
    panelOpen,
    running,
    messages,
  })
  const speech = companionSpeech(messages, running)
  const profile = disabled ? undefined : readableQueryData(profileQuery)

  const runInteraction = (interactionId: string) => {
    if (!owner || disabled || interactionMutation.blocked || queryAccessDenied(profileQuery)) return
    const animation = companionInteractionAnimation(pack.manifest, interactionId)
    if (animation) {
      window.clearTimeout(interactionTimerRef.current)
      setInteractionMotion((current) => ({
        animation,
        sequence: (current?.sequence ?? 0) + 1,
      }))
      interactionTimerRef.current = window.setTimeout(() => setInteractionMotion(undefined), 600)
    }
    if (!pack.local)
      interactionMutation.mutate({
        owner,
        input: {
          pluginId: pack.pluginId,
          interactionId,
          clientRevision: profile?.revision,
        },
      })
  }

  const companion = (
    <div
      className="soha-companion"
      data-state={visualState}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {bubbleEnabled && speech ? (
        <button className="soha-companion__bubble" type="button" onClick={onOpenAssistant}>
          {speech}
        </button>
      ) : null}
      <button
        aria-label={disabled ? '没有 AI 助手权限' : '打开 AI 宠物助手'}
        className="soha-companion__model"
        disabled={disabled}
        type="button"
        onClick={(event) => {
          const requestedInteraction =
            event.target instanceof Element
              ? event.target
                  .closest('[data-companion-interaction]')
                  ?.getAttribute('data-companion-interaction')
              : null
          runInteraction(companionInteractionId(pack.manifest, requestedInteraction))
          onOpenAssistant()
        }}
      >
        <CompanionRenderer interaction={interactionMotion} pack={pack} state={visualState} />
      </button>
      <div aria-label="宠物操作" className="soha-companion__actions" role="toolbar">
        <Button
          aria-label="摸摸宠物"
          disabled={disabled || interactionMutation.blocked || queryAccessDenied(profileQuery)}
          icon={<HeartOutlined />}
          loading={interactionMutation.isPending}
          shape="circle"
          size="small"
          onClick={() => runInteraction('pet')}
        />
        <Button
          aria-label="打开助手"
          disabled={disabled}
          icon={<MessageOutlined />}
          shape="circle"
          size="small"
          onClick={onOpenAssistant}
        />
        <Button
          aria-label="分析当前页面"
          disabled={disabled}
          icon={<SearchOutlined />}
          shape="circle"
          size="small"
          onClick={() => onAction('analyze-page')}
        />
        <Button
          aria-label="打开完整 AI Workbench"
          icon={<ExpandAltOutlined />}
          shape="circle"
          size="small"
          onClick={onOpenWorkbench}
        />
      </div>
      {!disabled && (interactionMutation.isError || profileQuery.isError) ? (
        <Popover
          content={
            <div style={{ maxWidth: 360 }}>
              {interactionMutation.feedback}
              <ReadFeedback query={profileQuery} label="宠物档案" />
            </div>
          }
          trigger="click"
        >
          <Button
            className="soha-companion__feedback"
            size="small"
            icon={<ExclamationCircleOutlined />}
            aria-label="查看宠物互动状态"
          >
            {interactionMutation.outcomeUnknown ? '互动待确认' : '查看状态'}
          </Button>
        </Popover>
      ) : null}
      {profile ? (
        <div
          aria-label={`等级 ${profile.level}，亲密度 ${profile.affinity}`}
          className="soha-companion__growth"
        >
          <span>Lv.{profile.level}</span>
          <span className="soha-companion__growth-track">
            <span style={{ width: `${Math.min(profile.xp % 100, 100)}%` }} />
          </span>
          <span>{profile.affinity}</span>
        </div>
      ) : null}
    </div>
  )

  if (nativeWindow) {
    return <div className="soha-companion-native-shell">{companion}</div>
  }

  return (
    <DraggableFloatShell
      className="soha-companion-shell"
      disabled={disabled}
      onDraggingChange={setDragging}
      shellSize={{ width: 176, height: 210 }}
      storageKey="soha.companion.float.position"
    >
      {companion}
    </DraggableFloatShell>
  )
}
