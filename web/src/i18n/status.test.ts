import { describe, expect, it } from 'vitest'
import { formatStatusLabel } from './status'

describe('formatStatusLabel', () => {
  it('localizes canonical lifecycle values and preserves unknown provider states', () => {
    expect(formatStatusLabel('Running', 'zh_CN')).toBe('运行中')
    expect(formatStatusLabel('not_deployed', 'zh_CN')).toBe('未部署')
    expect(formatStatusLabel('deployed', 'zh_CN')).toBe('已部署')
    expect(formatStatusLabel('NotReady', 'en_US')).toBe('Not Ready')
    expect(formatStatusLabel('unavailable', 'zh_CN')).toBe('不可用')
    expect(formatStatusLabel('agent_registered', 'zh_CN')).toBe('Agent 已注册')
    expect(formatStatusLabel('queued', 'zh_CN')).toBe('已排队')
    expect(formatStatusLabel('published', 'zh_CN')).toBe('已发布')
    expect(formatStatusLabel('attention', 'zh_CN')).toBe('需关注')
    expect(formatStatusLabel('normal', 'en_US')).toBe('Normal')
    expect(formatStatusLabel('reserved', 'zh_CN')).toBe('已预留')
    expect(formatStatusLabel('stopped', 'en_US')).toBe('Stopped')
    expect(formatStatusLabel('CrashLoopBackOff', 'zh_CN')).toBe('CrashLoopBackOff')
  })
})
