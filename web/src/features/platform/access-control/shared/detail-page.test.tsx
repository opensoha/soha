import { describe, expect, it } from 'vitest'
import type { ReactElement } from 'react'
import { renderAccessControlRuleSummaries } from './detail-page'

describe('access-control detail layout', () => {
  it('renders rule summaries as one flat list instead of nested cards', () => {
    const rendered = renderAccessControlRuleSummaries(['get pods'], 'empty') as ReactElement<{
      className?: string
    }>
    expect(rendered.props.className).toBe('soha-access-control-rule-list')
  })
})
