import { memoryEventMetadata } from './memory-feedback'
import type {
  WorkbenchSource,
  WorkbenchSendMessageStreamRequest,
  WorkbenchStreamEvent,
  WorkbenchToolCall,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { API_BASE_URL, getStoredAccessToken, refreshAuthSession } from '@/features/auth'

export interface WorkbenchSSEParseResult {
  events: WorkbenchStreamEvent[]
  rest: string
}

export interface WorkbenchStreamMessageState {
  id?: string
  sessionId?: string
  content: string
  done: boolean
  metadata?: Record<string, unknown>
}

export interface WorkbenchStreamThinkingState {
  summary: string
  collapsed: boolean
}

export interface WorkbenchStreamAgentStatusState {
  providerId: string
  providerKind: string
  status: string
  runId?: string
  metadata?: Record<string, unknown>
}

export interface WorkbenchStreamErrorState {
  message: string
  code?: string
  retryable?: boolean
}

export type WorkbenchStreamToolCall = WorkbenchToolCall & {
  outputLog?: string
}

export interface WorkbenchStreamState {
  message: WorkbenchStreamMessageState
  toolCalls: WorkbenchStreamToolCall[]
  artifacts: unknown[]
  sources: WorkbenchSource[]
  thinking?: WorkbenchStreamThinkingState
  agentStatus?: WorkbenchStreamAgentStatusState
  error?: WorkbenchStreamErrorState
  done: boolean
}

export class WorkbenchStreamTransportError extends Error {
  readonly code: string
  readonly retryable = true

  constructor(message: string, code: string) {
    super(message)
    this.name = 'WorkbenchStreamTransportError'
    this.code = code
  }
}

function parseSSEFrame(frame: string): WorkbenchStreamEvent[] {
  const data = frame
    .split(/\r?\n/)
    .filter((line) => line.startsWith('data:'))
    .map((line) => {
      const value = line.slice(5)
      return value.startsWith(' ') ? value.slice(1) : value
    })
    .join('\n')
    .trim()

  if (!data || data === '[DONE]') {
    return []
  }
  return [JSON.parse(data) as WorkbenchStreamEvent]
}

export function parseSSEChunk(chunk: string, rest = ''): WorkbenchSSEParseResult {
  const frames = `${rest}${chunk}`.split(/\r?\n\r?\n/)
  return {
    events: frames.slice(0, -1).flatMap(parseSSEFrame),
    rest: frames[frames.length - 1] ?? '',
  }
}

export function createWorkbenchStreamState(): WorkbenchStreamState {
  return {
    message: { content: '', done: false },
    toolCalls: [],
    artifacts: [],
    sources: [],
    done: false,
  }
}

export function canonicalWorkbenchAgentStatus(status?: string) {
  switch ((status || '').trim().toLowerCase()) {
    case 'pending':
    case 'queued':
      return 'queued'
    case 'running':
      return 'running'
    case 'completed':
    case 'complete':
    case 'success':
    case 'succeeded':
      return 'succeeded'
    case 'canceled':
    case 'cancelled':
    case 'client_cancelled':
      return 'cancelled'
    case 'callback_timeout':
    case 'error':
    case 'failed':
    case 'timeout':
      return 'failed'
    default:
      return status || ''
  }
}

export function isRunningWorkbenchAgentStatus(status?: string) {
  const canonical = canonicalWorkbenchAgentStatus(status)
  return canonical === 'queued' || canonical === 'running'
}

export function isTerminalWorkbenchAgentStatus(status?: string) {
  const canonical = canonicalWorkbenchAgentStatus(status)
  return canonical === 'succeeded' || canonical === 'failed' || canonical === 'cancelled'
}

function isTerminalWorkbenchStreamEvent(event: WorkbenchStreamEvent) {
  return (
    event.type === 'message.done' ||
    event.type === 'error' ||
    (event.type === 'agent.status' && isTerminalWorkbenchAgentStatus(event.status))
  )
}

export function isNativeRunHandoff(event: WorkbenchStreamEvent) {
  return (
    event.type === 'agent.status' &&
    ['native', 'dsh'].includes(event.providerKind) &&
    Boolean(event.runId) &&
    isRunningWorkbenchAgentStatus(event.status)
  )
}

export function workbenchStreamEventKey(
  event: Pick<WorkbenchStreamEvent, 'id' | 'sessionId' | 'sequence'> & { runId?: string },
) {
  return `${event.sessionId}:${event.runId ?? ''}:${event.sequence}:${event.id}`
}

function upsertToolCall(toolCalls: WorkbenchStreamToolCall[], next: WorkbenchStreamToolCall) {
  const index = toolCalls.findIndex((item) => item.id === next.id)
  if (index === -1) {
    return [...toolCalls, next]
  }
  return toolCalls.map((item, itemIndex) => (itemIndex === index ? { ...item, ...next } : item))
}

function upsertSource(sources: WorkbenchSource[], next: WorkbenchSource) {
  const index = sources.findIndex((item) => item.id === next.id)
  if (index === -1) {
    return [...sources, next]
  }
  return sources.map((item, itemIndex) => (itemIndex === index ? { ...item, ...next } : item))
}

function appendDelta(current: unknown, delta?: string) {
  if (!delta) return current
  if (typeof current === 'string') return `${current}${delta}`
  if (current === undefined || current === null) return delta
  return `${JSON.stringify(current)}${delta}`
}

function reduceToolDelta(
  toolCalls: WorkbenchStreamToolCall[],
  event: Extract<WorkbenchStreamEvent, { type: 'tool.delta' }>,
) {
  const existing = toolCalls.find((item) => item.id === event.toolCallId)
  const terminalStatus =
    existing?.status === 'success' || existing?.status === 'error' || existing?.status === 'skipped'
  const next: WorkbenchStreamToolCall = {
    ...existing,
    id: event.toolCallId,
    adapterId: existing?.adapterId ?? 'stream',
    toolName: existing?.toolName ?? event.toolCallId,
    status: terminalStatus ? existing.status : 'running',
    outputPreview: appendDelta(existing?.outputPreview, event.outputDelta),
    outputLog: appendDelta(existing?.outputLog, event.logDelta) as string | undefined,
  }
  return upsertToolCall(toolCalls, next)
}

export function reduceWorkbenchStreamState(
  state: WorkbenchStreamState,
  event: WorkbenchStreamEvent & { metadata?: Record<string, unknown> },
): WorkbenchStreamState {
  switch (event.type) {
    case 'message.delta':
      return {
        ...state,
        done: false,
        message: {
          ...state.message,
          id: event.messageId ?? state.message.id,
          sessionId: event.sessionId,
          content: `${event.messageId && state.message.id && event.messageId !== state.message.id ? '' : state.message.content}${event.contentDelta}`,
          done: false,
        },
      }
    case 'message.done':
      return {
        ...state,
        done: true,
        message: {
          ...state.message,
          id: event.messageId ?? state.message.id,
          sessionId: event.sessionId,
          content: event.content,
          done: true,
          metadata: event.metadata,
        },
      }
    case 'tool.started':
    case 'tool.completed':
      return {
        ...state,
        toolCalls: upsertToolCall(state.toolCalls, event.toolCall),
      }
    case 'tool.delta':
      return {
        ...state,
        done: false,
        toolCalls: reduceToolDelta(state.toolCalls, event),
      }
    case 'artifact.updated':
      return {
        ...state,
        artifacts: [...state.artifacts, event.artifact],
      }
    case 'source.updated':
      return {
        ...state,
        sources: upsertSource(state.sources, event.source),
      }
    case 'thinking.delta':
      return {
        ...state,
        thinking: {
          summary: `${state.thinking?.summary ?? ''}${event.textDelta}`,
          collapsed: false,
        },
      }
    case 'thinking.done':
      return {
        ...state,
        thinking: {
          summary: event.summary,
          collapsed: event.collapsed,
        },
      }
    case 'agent.status':
      return {
        ...state,
        done: isTerminalWorkbenchAgentStatus(event.status),
        agentStatus: {
          providerId: event.providerId,
          providerKind: event.providerKind,
          status: canonicalWorkbenchAgentStatus(event.status),
          metadata:
            memoryEventMetadata(event) ??
            (state.agentStatus?.runId === event.runId ? state.agentStatus?.metadata : undefined),
          ...(event.runId ? { runId: event.runId } : {}),
        },
      }
    case 'error':
      return {
        ...state,
        done: true,
        error: {
          message: event.message,
          code: event.code,
          retryable: event.retryable,
        },
        message: {
          ...state.message,
          sessionId: event.sessionId,
          done: true,
        },
      }
    default:
      return state
  }
}

export async function streamWorkbenchMessage(
  path: string,
  body: WorkbenchSendMessageStreamRequest,
  onEvent: (event: WorkbenchStreamEvent) => void | Promise<void>,
  signal?: AbortSignal,
) {
  const streamPath = path.startsWith('/') ? path : `/${path}`
  const request = () => {
    const headers = new Headers({
      Accept: 'text/event-stream',
      'Content-Type': 'application/json',
    })
    const accessToken = getStoredAccessToken()
    if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
    return fetch(`${API_BASE_URL}${streamPath}`, {
      method: 'POST',
      body: JSON.stringify(body),
      credentials: 'include',
      headers,
      signal,
    })
  }

  let response = await request()
  if (response.status === 401 && !signal?.aborted && (await refreshAuthSession())) {
    await response.body?.cancel().catch(() => undefined)
    response = await request()
  }

  if (!response.ok) {
    throw new Error(`Workbench stream failed: ${response.status} ${response.statusText}`)
  }
  if (!response.body) {
    throw new Error('Workbench stream response has no body')
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let rest = ''
  let terminalReceived = false
  let nativeRunHandedOff = false

  const dispatch = async (event: WorkbenchStreamEvent) => {
    terminalReceived ||= isTerminalWorkbenchStreamEvent(event)
    nativeRunHandedOff ||= isNativeRunHandoff(event)
    await onEvent(event)
  }

  const parseChunk = (chunk: string, buffered = '') => {
    try {
      return parseSSEChunk(chunk, buffered)
    } catch {
      throw new WorkbenchStreamTransportError(
        'Workbench stream returned an invalid event.',
        'stream_parse_error',
      )
    }
  }

  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) {
        break
      }
      const parsed = parseChunk(decoder.decode(value, { stream: true }), rest)
      rest = parsed.rest
      for (const event of parsed.events) {
        await dispatch(event)
      }
    }

    const parsed = parseChunk(`${decoder.decode()}\n\n`, rest)
    for (const event of parsed.events) {
      await dispatch(event)
    }
    if (!terminalReceived && !nativeRunHandedOff) {
      throw new WorkbenchStreamTransportError(
        'Workbench stream ended before a terminal event.',
        'stream_incomplete',
      )
    }
  } finally {
    reader.releaseLock()
  }
}
