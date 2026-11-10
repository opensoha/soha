import type { PersonalFeedFeedback, PersonalFeedSettings } from '@opensoha/contracts/gen/ts/sohaapi'
import { mutationOptions } from '@tanstack/react-query'

import {
  workbenchApi,
  type CreateInspectionTaskInput,
  type CreateWorkbenchSessionInput,
  type PatchWorkbenchSessionInput,
} from './api'
import { workbenchMutationKeys } from './keys'

export const workbenchMutations = {
  selectRuntimeProfile: () =>
    mutationOptions({
      mutationKey: workbenchMutationKeys.sessions('runtime-profile'),
      mutationFn: workbenchApi.runtimeProfiles.select,
      retry: false,
    }),
  runtime: (assistantId: string) => mutationOptions({
    mutationKey: ['copilot', 'workspace-runtime', assistantId],
    mutationFn: (input: Parameters<typeof workbenchApi.assistant.runtime>[1]) => workbenchApi.assistant.runtime(assistantId, input),
    retry: false,
  }),
  guestTools: () => mutationOptions({mutationKey: ['copilot', 'guest-tools'], mutationFn: workbenchApi.assistant.guestTools, retry: false}),
  messageWindow: () =>
    mutationOptions({
      mutationKey: workbenchMutationKeys.sessions('window'),
      mutationFn: ({
        sessionId,
        selector,
      }: {
        sessionId: string
        selector: { before?: string; after?: string }
        historyKey: string
      }) => workbenchApi.messageWindow(sessionId, selector),
    }),
  feed: {
    settings: () =>
      mutationOptions({
        mutationKey: workbenchMutationKeys.feed('settings'),
        mutationFn: (input: PersonalFeedSettings) => workbenchApi.feed.settings(input),
      }),
    refresh: () =>
      mutationOptions({
        mutationKey: workbenchMutationKeys.feed('refresh'),
        mutationFn: workbenchApi.feed.refresh,
      }),
    feedback: () =>
      mutationOptions({
        mutationKey: workbenchMutationKeys.feed('feedback'),
        mutationFn: ({ id, input }: { id: string; input: PersonalFeedFeedback }) =>
          workbenchApi.feed.feedback(id, input),
      }),
  },
  sessions: {
    create: () =>
      mutationOptions({
        mutationKey: workbenchMutationKeys.sessions('create'),
        mutationFn: (input: CreateWorkbenchSessionInput) => workbenchApi.sessions.create(input),
      }),
    patch: () =>
      mutationOptions({
        mutationKey: workbenchMutationKeys.sessions('patch'),
        mutationFn: (input: PatchWorkbenchSessionInput) => workbenchApi.sessions.patch(input),
      }),
    archive: () =>
      mutationOptions({
        mutationKey: workbenchMutationKeys.sessions('archive'),
        mutationFn: (sessionId: string) => workbenchApi.sessions.archive(sessionId),
      }),
    createInspectionTask: () =>
      mutationOptions({
        mutationKey: workbenchMutationKeys.sessions('create-inspection-task'),
        mutationFn: (input: CreateInspectionTaskInput) =>
          workbenchApi.sessions.createInspectionTask(input),
      }),
  },
}
