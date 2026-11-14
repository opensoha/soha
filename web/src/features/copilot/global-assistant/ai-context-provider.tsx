import { createContext, useContext, useEffect, useRef } from 'react'
import { useLocation } from 'react-router-dom'
import type { AIGlobalAssistantLaunchRequest, AIPageContext } from './ai-context'
import { contextIdentityKey } from './ai-context'

export interface AIPageContextRegistryValue {
  currentContext: AIPageContext
  launchAssistant: (request: AIGlobalAssistantLaunchRequest) => Promise<void>
  openAssistant: () => void
  openWorkbench: () => void
  registerPageContext: (id: string, context: AIPageContext, key: string) => () => void
}

export const AIPageContextRegistry = createContext<AIPageContextRegistryValue | null>(null)

function nextContextId() {
  return `ai-context-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export type AIPageContextInput = Omit<AIPageContext, 'sourceRoute'> & {
  sourceRoute?: string
}

export function useAIPageContext(context: AIPageContextInput, enabled = true) {
  const registry = useContext(AIPageContextRegistry)
  const location = useLocation()
  const registerPageContext = registry?.registerPageContext
  const idRef = useRef(nextContextId())
  const resolvedContext: AIPageContext = {
    ...context,
    sourceRoute: context.sourceRoute ?? `${location.pathname}${location.search}`,
  }
  const contextRef = useRef(resolvedContext)
  const key = contextIdentityKey(resolvedContext)
  contextRef.current = resolvedContext

  useEffect(() => {
    if (!enabled || !registerPageContext) return undefined
    return registerPageContext(idRef.current, contextRef.current, key)
  }, [enabled, key, registerPageContext])
}

export function useAIGlobalAssistant() {
  return useContext(AIPageContextRegistry)
}
