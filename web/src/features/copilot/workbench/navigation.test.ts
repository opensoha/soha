import { expect, it } from 'vitest'
import { getAIWorkbenchPathForMode, getAIWorkbenchPathForSession } from './navigation'

it('opens main, side and historical chats without inheriting a professional task mode', () => {
  expect(
    getAIWorkbenchPathForSession({
      id: 'heya-main',
      metadata: { conversationKind: 'personal_main' },
    }),
  ).toBe('/heya?session=heya-main')
  expect(
    getAIWorkbenchPathForSession({
      id: 'heya-side',
      metadata: { conversationKind: 'personal_side' },
    }),
  ).toBe('/heya?session=heya-side')
  expect(
    getAIWorkbenchPathForSession(
      { id: 'main-1', metadata: { conversationKind: 'main', mode: 'root_cause' } },
      '?clusterId=cluster-a&namespace=payments&mode=root_cause',
    ),
  ).toBe('/ai-workbench/chat?session=main-1&clusterId=cluster-a&namespace=payments')
  expect(getAIWorkbenchPathForSession({ id: 'legacy-1', metadata: { mode: 'performance' } })).toBe(
    '/ai-workbench/chat?session=legacy-1',
  )
  expect(
    getAIWorkbenchPathForSession({
      id: 'side-1',
      metadata: { conversationKind: 'side', mode: 'trace' },
    }),
  ).toBe('/ai-workbench/chat?session=side-1')
  expect(getAIWorkbenchPathForMode('performance', '?session=legacy-1')).toBe(
    '/ai-workbench/performance?session=legacy-1',
  )
})
