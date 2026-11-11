import { useState, type SetStateAction } from 'react'
import type { AssistantChatTask } from '../types'
import type {
  WorkbenchContextSelection,
  WorkbenchModelPreferences,
} from '@opensoha/contracts/gen/ts/sohaapi'

type ChatDraft = {
  value: string
  context: WorkbenchContextSelection
  knowledge: string[]
  heldValue?: string
  task?: AssistantChatTask
  model?: WorkbenchModelPreferences
}
const emptyDraft: ChatDraft = { value: '', context: {}, knowledge: [] }

export function useChatDraft(sessionId?: string) {
  const [drafts, setDrafts] = useState<Record<string, ChatDraft>>({})
  const key = sessionId ?? ''
  const draft = drafts[key] ?? emptyDraft
  const updateDraft = (
    patch: Partial<ChatDraft> | ((draft: ChatDraft) => Partial<ChatDraft>),
    id = key,
  ) => {
    setDrafts((current) => {
      const before = current[id] ?? emptyDraft
      return {
        ...current,
        [id]: { ...before, ...(typeof patch === 'function' ? patch(before) : patch) },
      }
    })
  }
  const setSenderValue = (value: SetStateAction<string>) =>
    updateDraft((current) => ({
      value: typeof value === 'function' ? value(current.value) : value,
    }))
  const setContextSelection = (context: SetStateAction<WorkbenchContextSelection>) =>
    updateDraft((current) => ({
      context: typeof context === 'function' ? context(current.context) : context,
    }))
  const setKnowledgeBaseIds = (knowledge: string[]) => updateDraft({ knowledge })
  return {
    heldValue: draft.heldValue,
    assistantTask: draft.task,
    senderValue: draft.value,
    contextSelection: draft.context,
    knowledgeBaseIds: draft.knowledge,
    modelDraft: draft.model,
    setSenderValue,
    setContextSelection,
    setKnowledgeBaseIds,
    updateDraft,
  }
}
