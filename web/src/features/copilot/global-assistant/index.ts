export type {
  AIGlobalAssistantAction,
  AIGlobalAssistantLaunchRequest,
  AIGlobalAssistantMessage,
  AIPageContext,
  AISelectedTextKind,
  AISelectionContext,
  AIWorkbenchSource,
} from './ai-context'
export {
  contextIdentityKey,
  encodeAIContextForElement,
  inferSelectionKind,
  sanitizeSelectionText,
  workbenchScopeFromAIContext,
} from './ai-context'
export { useAIGlobalAssistant, useAIPageContext } from './ai-context-provider'
export {
  GlobalAIAssistantProvider,
  type GlobalAssistantCompanionRenderProps,
} from './ai-global-assistant-provider'
export {
  clampFloatPosition,
  defaultFloatPosition,
  DraggableFloatShell,
  snapFloatPosition,
  type FloatPosition,
} from './draggable-float-shell'
