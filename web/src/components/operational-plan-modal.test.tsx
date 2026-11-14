/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import type { OperationalPlan } from '@opensoha/contracts/gen/ts/sohaapi'
import { OperationalPlanModal } from './operational-plan-modal'

vi.mock('antd', () => ({
  Alert: ({ title }: { title?: ReactNode }) => <div>{title}</div>,
  Checkbox: ({
    checked,
    children,
    onChange,
  }: {
    checked?: boolean
    children?: ReactNode
    onChange?: (event: { target: { checked: boolean } }) => void
  }) => (
    <label>
      <input
        aria-label="approval-confirmation"
        checked={checked}
        type="checkbox"
        onChange={(event) => onChange?.({ target: { checked: event.target.checked } })}
      />
      {children}
    </label>
  ),
  Descriptions: ({
    items,
    title,
  }: {
    items?: Array<{ children?: ReactNode }>
    title?: ReactNode
  }) => (
    <div>
      {title}
      {items?.map((item, index) => (
        <div key={index}>{item.children}</div>
      ))}
    </div>
  ),
  Modal: ({
    children,
    okButtonProps,
    onOk,
    open,
  }: {
    children?: ReactNode
    okButtonProps?: { disabled?: boolean }
    onOk?: () => void
    open?: boolean
  }) =>
    open ? (
      <div>
        <button data-testid="confirm" disabled={okButtonProps?.disabled} onClick={onOk}>
          confirm
        </button>
        {children}
      </div>
    ) : null,
}))

vi.mock('./status-tag', () => ({
  StatusTag: ({ value }: { value?: string }) => <span>{value}</span>,
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

describe('OperationalPlanModal', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
  })

  it('renders legacy plans whose collection fields are null', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    const plan = {
      capability: 'docker.project.deploy',
      target: 'nginx-open-source-stack',
      ready: true,
      riskLevel: 'low',
      requiresApproval: false,
      changes: null,
      warnings: null,
    } as unknown as OperationalPlan

    await act(async () => {
      root.render(
        <OperationalPlanModal
          onCancel={() => undefined}
          onConfirm={() => undefined}
          plan={plan}
          title="部署确认"
        />,
      )
    })

    expect(container.textContent).toContain('nginx-open-source-stack')
    expect(container.textContent).toContain('计划变更')
  })

  it('renders Kubernetes field ownership and conflicts', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    const plan = {
      capability: 'k8s.resources.update',
      target: 'cluster-a/team-a/Deployment/api',
      ready: false,
      riskLevel: 'mutate',
      requiresApproval: false,
      changes: [
        {
          action: 'apply',
          resource: 'Deployment/api',
          summary: 'apply manifest',
          sensitiveValuesRedacted: false,
        },
      ],
      warnings: [],
      kubernetesResourceUpdate: {
        fieldManager: 'opensoha-resource-edit/v1',
        changedFields: ['/spec/replicas'],
        owners: [
          {
            manager: 'helm',
            operation: 'Apply',
            apiVersion: 'apps/v1',
            fields: ['/spec/replicas'],
          },
        ],
        conflicts: [
          { field: '/spec/replicas', manager: 'helm', message: 'field is owned by helm' },
        ],
      },
    } as OperationalPlan

    await act(async () => {
      root.render(
        <OperationalPlanModal
          onCancel={() => undefined}
          onConfirm={() => undefined}
          plan={plan}
          title="更新确认"
        />,
      )
    })

    expect(container.textContent).toContain('opensoha-resource-edit/v1')
    expect(container.textContent).toContain('/spec/replicas')
    expect(container.textContent).toContain('helm')
  })

  it('requires explicit confirmation for approval plans', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    const onConfirm = vi.fn()
    const plan = {
      capability: 'k8s.helm.releases.rollback',
      target: 'cluster-a/team-a/gateway',
      ready: true,
      riskLevel: 'high',
      requiresApproval: true,
      changes: [],
      warnings: [],
    } as OperationalPlan

    await act(async () => {
      root.render(
        <OperationalPlanModal
          onCancel={() => undefined}
          onConfirm={onConfirm}
          plan={plan}
          title="回滚确认"
        />,
      )
    })

    const confirm = container.querySelector<HTMLButtonElement>('[data-testid="confirm"]')
    expect(confirm?.disabled).toBe(true)
    await act(async () => {
      container.querySelector<HTMLInputElement>('[aria-label="approval-confirmation"]')?.click()
    })
    expect(confirm?.disabled).toBe(false)
    await act(async () => confirm?.click())
    expect(onConfirm).toHaveBeenCalledOnce()
  })
})
