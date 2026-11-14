import { describe, expect, it } from 'vitest'
import {
  AI_WORKBENCH_CONFIG_KEY,
  aiWorkbenchDraft,
  aiWorkbenchEnabled,
  GLOBAL_ASSISTANT_CONFIG_KEY,
  isAIWorkbenchChildConfig,
  isAIWorkbenchConfig,
} from './ai-module-model'
import type { RuntimeConfigItem } from './types'

const items = [
  { key: AI_WORKBENCH_CONFIG_KEY, effectiveValue: true },
  { key: GLOBAL_ASSISTANT_CONFIG_KEY, effectiveValue: true },
] as RuntimeConfigItem[]

describe('AI module model', () => {
  it('derives the parent state from the AI workbench key', () => {
    expect(aiWorkbenchEnabled(items, {})).toBe(true)
    expect(aiWorkbenchEnabled(items, { [AI_WORKBENCH_CONFIG_KEY]: false })).toBe(false)
  })

  it('cascades disable to the assistant without enabling it with the parent', () => {
    expect(aiWorkbenchDraft(false)).toEqual({
      [AI_WORKBENCH_CONFIG_KEY]: false,
      [GLOBAL_ASSISTANT_CONFIG_KEY]: false,
    })
    expect(aiWorkbenchDraft(true)).toEqual({ [AI_WORKBENCH_CONFIG_KEY]: true })
  })

  it('classifies the workbench branch and its child', () => {
    expect(isAIWorkbenchConfig(AI_WORKBENCH_CONFIG_KEY)).toBe(true)
    expect(isAIWorkbenchConfig(GLOBAL_ASSISTANT_CONFIG_KEY)).toBe(true)
    expect(isAIWorkbenchChildConfig(GLOBAL_ASSISTANT_CONFIG_KEY)).toBe(true)
    expect(isAIWorkbenchChildConfig(AI_WORKBENCH_CONFIG_KEY)).toBe(false)
  })
})
