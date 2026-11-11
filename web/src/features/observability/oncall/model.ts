import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import {
  emptyPayloadMap,
  isObservabilityPayloadMap,
  parseObservabilityJson,
  toText,
} from '../shared/json'
import type {
  OnCallAssignmentFormValues,
  OnCallAssignmentPayload,
  OnCallDayAssignment,
  OnCallEscalationPolicyFormValues,
  OnCallEscalationPolicyPayload,
  OnCallEscalationStepFormValues,
  OnCallEscalationStepPayload,
  OnCallRotation,
  OnCallRotationConfig,
  OnCallRotationFormValues,
  OnCallRotationMode,
  OnCallRotationPayload,
  OnCallScheduleFormValues,
  OnCallSchedulePayload,
  OnCallUser,
  ShiftSlot,
} from './types'

export const ONCALL_DATE_FORMAT = 'YYYY-MM-DD'

interface OnCallRotationOverrides {
  [dateKey: string]: string[]
}

function stringListFromField(value: unknown) {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
  return String(value || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function normalizeParticipantList(value: unknown): string[] {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
  if (typeof value !== 'string') return []
  const text = value.trim()
  if (!text) return []
  if (text.startsWith('[')) {
    try {
      const parsed = JSON.parse(text)
      if (Array.isArray(parsed)) return normalizeParticipantList(parsed)
    } catch {
      // Preserve comma-separated values when the input only resembles JSON.
    }
  }
  return text
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export function readRotationOverrides(rotationConfig?: OnCallRotationConfig) {
  const rawOverrides = rotationConfig?.overrides
  if (!isObservabilityPayloadMap(rawOverrides)) return {}
  return Object.entries(rawOverrides).reduce<OnCallRotationOverrides>(
    (result, [dateKey, value]) => {
      if (!/^\d{4}-\d{2}-\d{2}$/.test(dateKey)) return result
      const participants = isObservabilityPayloadMap(value)
        ? normalizeParticipantList(
            value.participants ?? value.currentParticipants ?? value.currentParticipant,
          )
        : normalizeParticipantList(value)
      if (participants.length > 0) result[dateKey] = participants
      return result
    },
    {},
  )
}

export function buildRotationConfigWithOverride(
  rotationConfig: OnCallRotationConfig | undefined,
  dateKey: string,
  participants: string[],
): OnCallRotationConfig {
  const nextConfig: OnCallRotationConfig = { ...(rotationConfig ?? {}) }
  const overrides = { ...readRotationOverrides(rotationConfig) }
  if (participants.length > 0) overrides[dateKey] = participants
  else delete overrides[dateKey]
  nextConfig.overrides = overrides
  return nextConfig
}

function readPositiveNumber(value: unknown, fallback: number) {
  const numeric = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(numeric) && numeric > 0 ? numeric : fallback
}

function parseDayjs(value: unknown) {
  if (typeof value !== 'string' && typeof value !== 'number' && !(value instanceof Date)) {
    return null
  }
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed : null
}

function rotationShiftMinutes(rotation: OnCallRotation) {
  const rotationMinutes = readPositiveNumber(rotation.rotationConfig?.rotationMinutes, 0)
  if (rotationMinutes > 0) return rotationMinutes
  return readPositiveNumber(rotation.rotationConfig?.shiftHours, 24) * 60
}

function rotationStartAt(rotation: OnCallRotation, date: Dayjs) {
  return (
    parseDayjs(rotation.rotationConfig?.startAt) ??
    parseDayjs(rotation.createdAt) ??
    date.startOf('day')
  )
}

function baseParticipantsForDate(rotation: OnCallRotation, date: Dayjs) {
  const participants = normalizeParticipantList(rotation.participants)
  if (participants.length === 0) return []
  const shiftMinutes = rotationShiftMinutes(rotation)
  const startAt = rotationStartAt(rotation, date)
  const dayStart = date.startOf('day')
  const dayEnd = dayStart.add(1, 'day')
  const elapsedAtDayStart = dayStart.diff(startAt, 'minute')
  let slot = elapsedAtDayStart < 0 ? 0 : Math.floor(elapsedAtDayStart / shiftMinutes)
  let slotStart = startAt.add(slot * shiftMinutes, 'minute')
  while (slotStart.isAfter(dayStart) && slot > 0) {
    slot -= 1
    slotStart = startAt.add(slot * shiftMinutes, 'minute')
  }
  const result: string[] = []
  const seen = new Set<string>()
  let guard = 0
  while (slotStart.isBefore(dayEnd) && guard < 200) {
    const slotEnd = slotStart.add(shiftMinutes, 'minute')
    if (slotEnd.isAfter(dayStart) || slotEnd.isSame(dayStart)) {
      const participant = participants[slot < 0 ? 0 : slot % participants.length]
      if (participant && !seen.has(participant)) {
        seen.add(participant)
        result.push(participant)
      }
    }
    slot += 1
    slotStart = slotEnd
    guard += 1
  }
  return result.length > 0 ? result : [participants[0]]
}

export function assignmentForDate(
  rotation: OnCallRotation | null,
  date: Dayjs,
): OnCallDayAssignment {
  if (!rotation) return { participants: [], override: false }
  const overrides = readRotationOverrides(rotation.rotationConfig)
  const dateKey = date.format(ONCALL_DATE_FORMAT)
  if (overrides[dateKey]) return { participants: overrides[dateKey], override: true }
  return { participants: baseParticipantsForDate(rotation, date), override: false }
}

export function shiftsForDate(rotation: OnCallRotation | null, date: Dayjs): ShiftSlot[] {
  if (!rotation) return []
  const participants = normalizeParticipantList(rotation.participants)
  if (participants.length === 0) return []
  const shiftMinutes = rotationShiftMinutes(rotation)
  const startAt = rotationStartAt(rotation, date)
  const dayStart = date.startOf('day')
  const dayEnd = dayStart.add(1, 'day')
  const elapsedAtDayStart = dayStart.diff(startAt, 'minute')
  let slot = elapsedAtDayStart < 0 ? 0 : Math.floor(elapsedAtDayStart / shiftMinutes)
  let slotStart = startAt.add(slot * shiftMinutes, 'minute')
  while (slotStart.isAfter(dayStart) && slot > 0) {
    slot -= 1
    slotStart = startAt.add(slot * shiftMinutes, 'minute')
  }
  const result: ShiftSlot[] = []
  let guard = 0
  while (slotStart.isBefore(dayEnd) && guard < 200) {
    const slotEnd = slotStart.add(shiftMinutes, 'minute')
    if (slotEnd.isAfter(dayStart)) {
      const displayStart = slotStart.isBefore(dayStart) ? dayStart : slotStart
      const displayEnd = slotEnd.isAfter(dayEnd) ? dayEnd : slotEnd
      result.push({
        participant: participants[slot < 0 ? 0 : slot % participants.length],
        start: displayStart.format('HH:mm'),
        end: displayEnd.format('HH:mm'),
      })
    }
    slot += 1
    slotStart = slotEnd
    guard += 1
  }
  return result
}

export function rotationConfigShiftHours(rotationConfig?: OnCallRotationConfig) {
  const rotationMinutes = readPositiveNumber(rotationConfig?.rotationMinutes, 0)
  if (rotationMinutes > 0) return Number((rotationMinutes / 60).toFixed(2))
  return readPositiveNumber(rotationConfig?.shiftHours, 24)
}

export function rotationModeFromConfig(rotationConfig?: OnCallRotationConfig): OnCallRotationMode {
  const shiftHours = rotationConfigShiftHours(rotationConfig)
  if (Math.abs(shiftHours - 24) < 0.001) return 'daily'
  if (Math.abs(shiftHours - 168) < 0.001) return 'weekly'
  return 'custom'
}

export function rotationModeLabel(rotationConfig?: OnCallRotationConfig) {
  const mode = rotationModeFromConfig(rotationConfig)
  if (mode === 'daily') return '每日轮换'
  if (mode === 'weekly') return '每周轮换'
  return `自定义 · ${rotationConfigShiftHours(rotationConfig)} 小时`
}

export function defaultOnCallRotationFormValues(): OnCallRotationFormValues {
  return {
    name: '',
    scheduleId: '',
    participants: [],
    rotationMode: 'daily',
    shiftHours: 24,
    startAt: dayjs(),
    enabled: true,
  }
}

export function toOnCallRotationFormValues(
  record: OnCallRotation | null,
): OnCallRotationFormValues {
  if (!record) return defaultOnCallRotationFormValues()
  return {
    name: record.name,
    scheduleId: record.scheduleId,
    participants: normalizeParticipantList(record.participants),
    rotationMode: rotationModeFromConfig(record.rotationConfig),
    shiftHours: rotationConfigShiftHours(record.rotationConfig),
    startAt: parseDayjs(record.rotationConfig?.startAt) ?? parseDayjs(record.createdAt),
    enabled: record.enabled,
  }
}

export function defaultEscalationStep(): OnCallEscalationStepFormValues {
  return { scheduleId: '', delayMinutes: 0, role: '', description: '' }
}

export function toOnCallEscalationStepFormValues(steps?: OnCallEscalationStepPayload[]) {
  if (!steps?.length) return [defaultEscalationStep()]
  return steps.map((step) => ({
    scheduleId: typeof step.scheduleId === 'string' ? step.scheduleId : '',
    delayMinutes: Number(step.delayMinutes ?? 0),
    role: typeof step.role === 'string' ? step.role : '',
    description: typeof step.description === 'string' ? step.description : '',
  }))
}

export function buildOnCallSchedulePayload(
  values: OnCallScheduleFormValues,
): OnCallSchedulePayload {
  return {
    name: toText(values.name),
    timeZone: toText(values.timeZone),
    description: toText(values.description),
    enabled: Boolean(values.enabled),
  }
}

export function buildOnCallRotationPayload(
  values: OnCallRotationFormValues,
  currentConfig?: OnCallRotationConfig,
): OnCallRotationPayload {
  const rotationConfig: OnCallRotationConfig = { ...(currentConfig ?? {}) }
  delete rotationConfig.shiftHours
  delete rotationConfig.rotationMinutes
  delete rotationConfig.startAt
  const shiftHours =
    values.rotationMode === 'daily'
      ? 24
      : values.rotationMode === 'weekly'
        ? 168
        : readPositiveNumber(values.shiftHours, 24)
  rotationConfig.shiftHours = shiftHours
  if (values.rotationMode === 'custom') {
    rotationConfig.rotationMinutes = Math.max(1, Math.round(shiftHours * 60))
  }
  if (values.startAt?.isValid()) rotationConfig.startAt = values.startAt.toISOString()
  return {
    name: toText(values.name),
    scheduleId: toText(values.scheduleId),
    participants: normalizeParticipantList(values.participants),
    rotationConfig,
    enabled: Boolean(values.enabled),
  }
}

export function buildOnCallEscalationPolicyPayload(
  values: OnCallEscalationPolicyFormValues,
  currentSteps?: OnCallEscalationStepPayload[],
): OnCallEscalationPolicyPayload {
  return {
    name: toText(values.name),
    steps: (values.steps ?? []).map((step, index) => ({
      ...(isObservabilityPayloadMap(currentSteps?.[index]) ? currentSteps?.[index] : {}),
      scheduleId: step.scheduleId || '',
      delayMinutes: Number(step.delayMinutes ?? 0),
      role: step.role || '',
      description: step.description || '',
    })),
    enabled: Boolean(values.enabled),
  }
}

export function buildOnCallAssignmentPayload(
  values: OnCallAssignmentFormValues,
): OnCallAssignmentPayload {
  return {
    name: toText(values.name),
    integrationId: toText(values.integrationId),
    integrationType: toText(values.integrationType),
    businessLineId: toText(values.businessLineId),
    alertCategory: toText(values.alertCategory),
    alertName: toText(values.alertName),
    severity: toText(values.severity),
    service: toText(values.service),
    role: toText(values.role),
    matchers: parseObservabilityJson(toText(values.matchers || '{}'), emptyPayloadMap()),
    targetType: values.targetType === 'schedule' ? 'schedule' : 'escalation',
    targetRef: toText(values.targetRef),
    routeOrder: Number(values.routeOrder ?? 100),
    groupBy: stringListFromField(values.groupBy),
    priority: Number(values.priority ?? 100),
    enabled: Boolean(values.enabled),
  }
}

export function onCallUserOptions(users: OnCallUser[]) {
  return users.map((user) => {
    const username = user.username || user.email || user.id
    const display = user.displayName || username
    return { value: username, label: display !== username ? `${display} (${username})` : username }
  })
}

export function participantAvatarText(value: string) {
  const parts = value
    .split(/[\s._-]+/)
    .map((item) => item.trim())
    .filter(Boolean)
  if (parts.length >= 2) return `${parts[0][0] || ''}${parts[1][0] || ''}`.toUpperCase()
  return value.slice(0, 2).toUpperCase()
}

export function formatParticipantSummary(participants: string[]) {
  return participants.length > 0 ? participants.join('、') : '未排班'
}
