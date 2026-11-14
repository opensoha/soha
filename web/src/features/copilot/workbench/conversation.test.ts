import { expect, it } from 'vitest'
import { assistantReactionForMessage, type ConversationMessage } from './conversation'

it('binds a persisted AI reaction only to the same-session user message', () => {
  const user: ConversationMessage = {
    id: 'user',
    sessionId: 'owned',
    role: 'user',
    content: 'hi',
    createdAt: '',
  }
  const reply: ConversationMessage = {
    ...user,
    id: 'reply',
    role: 'assistant',
    metadata: { requestMessageId: user.id, assistantReaction: '👋' },
  }
  expect(assistantReactionForMessage([user, reply], user)).toBe('👋')
  expect(assistantReactionForMessage([user, reply], reply)).toBeUndefined()
  expect(
    assistantReactionForMessage([user, { ...reply, sessionId: 'foreign' }], user),
  ).toBeUndefined()
  expect(
    assistantReactionForMessage([user, { ...reply, deliveryStatus: 'abort' }], user),
  ).toBeUndefined()
  expect(
    assistantReactionForMessage(
      [user, { ...reply, metadata: { requestMessageId: user.id, assistantReaction: '<script>' } }],
      user,
    ),
  ).toBeUndefined()
})
