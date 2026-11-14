/** @vitest-environment jsdom */

import { describe, expect, it } from 'vitest'
import {
  buildMenuSectionOptions,
  normalizeMenuSection,
  resolveMenuSectionLabel,
} from './menu-schema'

describe('menu section schema helpers', () => {
  it('normalizes legacy display labels to canonical section keys', () => {
    expect(normalizeMenuSection('Dashboard')).toBe('platform')
    expect(normalizeMenuSection('observe')).toBe('ops')
    expect(normalizeMenuSection('logs')).toBe('observe-signals')
    expect(normalizeMenuSection('observability-data')).toBe('observe-data')
    expect(normalizeMenuSection('observability-dashboards')).toBe('dashboards')
    expect(normalizeMenuSection('alerts')).toBe('alerting')
    expect(normalizeMenuSection('ai-operations')).toBe('ai-governance')
    expect(normalizeMenuSection('Delivery')).toBe('delivery')
    expect(normalizeMenuSection('extension')).toBe('extensions')
    expect(normalizeMenuSection('system-integrations')).toBe('integrations')
    expect(normalizeMenuSection('Endpoint')).toBe('endpoint')
    expect(normalizeMenuSection('VPN')).toBe('vpn')
    expect(normalizeMenuSection('control')).toBe('control')
  })

  it('renders canonical labels for normalized aliases', () => {
    expect(resolveMenuSectionLabel('observe')).toBe('Observe')
    expect(resolveMenuSectionLabel('logging')).toBe('探索')
    expect(resolveMenuSectionLabel('observe-data')).toBe('数据与集成')
    expect(resolveMenuSectionLabel('dashboards')).toBe('仪表盘')
    expect(resolveMenuSectionLabel('dashboards', 'en_US')).toBe('Dashboards')
    expect(resolveMenuSectionLabel('alerting')).toBe('告警与响应')
    expect(resolveMenuSectionLabel('alerting', 'en_US')).toBe('Alerting & Response')
    expect(resolveMenuSectionLabel('Dashboard')).toBe('Dashboard')
    expect(resolveMenuSectionLabel('extensions')).toBe('扩展')
    expect(resolveMenuSectionLabel('extensions', 'en_US')).toBe('Extensions')
    expect(resolveMenuSectionLabel('integrations')).toBe('系统集成')
    expect(resolveMenuSectionLabel('integrations', 'en_US')).toBe('System Integrations')
    expect(resolveMenuSectionLabel('users')).toBe('用户管理')
    expect(resolveMenuSectionLabel('users', 'en_US')).toBe('User Management')
    expect(resolveMenuSectionLabel('network')).toBe('网络准入')
    expect(resolveMenuSectionLabel('endpoint')).toBe('终端')
    expect(resolveMenuSectionLabel('endpoint', 'en_US')).toBe('Endpoints')
    expect(resolveMenuSectionLabel('vpn')).toBe('零信任网络与 VPN')
    expect(resolveMenuSectionLabel('vpn', 'en_US')).toBe('Zero Trust Network & VPN')
    expect(resolveMenuSectionLabel('proxy')).toBe('代理')
    expect(resolveMenuSectionLabel('control')).toBe('control')
    expect(resolveMenuSectionLabel('ai-interaction')).toBe('交互')
    expect(resolveMenuSectionLabel('ai-engineering')).toBe('AI 工程')
    expect(resolveMenuSectionLabel('ai-model-access')).toBe('模型与接入')
    expect(resolveMenuSectionLabel('ai-governance')).toBe('治理与可观测')
  })

  it('only keeps active section options and removes duplicate aliases', () => {
    expect(
      buildMenuSectionOptions([
        'platform',
        'Dashboard',
        'observe',
        'ops',
        'logs',
        'alerts',
        'control',
        'deliver',
      ]),
    ).toEqual([
      { value: 'platform', label: 'Dashboard' },
      { value: 'ops', label: 'Observe' },
      { value: 'observe-signals', label: '探索' },
      { value: 'alerting', label: '告警与响应' },
      { value: 'delivery', label: '持续交付' },
      { value: 'control', label: 'control' },
    ])
  })
})
