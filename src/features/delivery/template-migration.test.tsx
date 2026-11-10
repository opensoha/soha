/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { Form, type FormInstance } from 'antd'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import {
  TemplateMigrationFields,
  MigrationAssessment,
  migrationPolicyInput,
  planMigrations,
} from './template-migration'
import {
  buildTemplateDocument,
  workflowTemplateDocument,
  deploymentTemplateDocument,
} from './documents/model'

let root: Root
let host: HTMLDivElement
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener: vi.fn(), removeListener: vi.fn() })
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})

it('distinguishes verified first deployment from an unknown existing migration baseline', async () => {
  const value = {
    fromVersion: 0,
    toVersion: 4,
    initialization: true,
    reason: '首次发布需审批',
    requiresApproval: true,
    blocked: false,
    steps: [],
  }
  await act(async () => root.render(<MigrationAssessment value={value} />))
  expect(host.textContent).toContain('首次发布 → v4 · 待确认运行基线')
  expect(host.textContent).toContain('原始资源 UID 与健康状态')
  expect(host.textContent).not.toContain('迁移基线 未知')
  await act(async () =>
    root.render(<MigrationAssessment value={{ ...value, initialization: false, blocked: true }} />),
  )
  expect(host.textContent).toContain('迁移基线 未知 → v4')
  expect(host.textContent).not.toContain('首次发布 →')
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('requires a nonblank guide when either migration declaration is enabled', async () => {
  let form!: FormInstance
  function Harness() {
    ;[form] = Form.useForm()
    return (
      <Form form={form}>
        <TemplateMigrationFields />
      </Form>
    )
  }
  await act(async () => root.render(<Harness />))
  await act(async () =>
    form.setFieldsValue({
      migrationPolicy: { sequential: true, irreversible: false, guide: '  ' },
    }),
  )
  let failed = false
  await act(async () => {
    try {
      await form.validateFields()
    } catch {
      failed = true
    }
  })
  expect(failed).toBe(true)
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 50))
  })
  expect(host.textContent).toContain('请填写迁移前提、验证方式和恢复方案')
  await act(async () => form.setFieldValue(['migrationPolicy', 'guide'], ' 已核实备份与恢复方案 '))
  await act(async () => {
    await form.validateFields()
  })
  expect(migrationPolicyInput(form.getFieldValue('migrationPolicy'))).toEqual({
    sequential: true,
    irreversible: false,
    guide: '已核实备份与恢复方案',
  })
  expect(migrationPolicyInput(undefined)).toBeUndefined()
})

it('preserves declarations in all template document forms and displays intermediate boundaries', async () => {
  const policy = { sequential: true, irreversible: true, guide: '备份后逐版执行，仅允许前向修复' }
  const values = [
    buildTemplateDocument({
      name: 'Build',
      buildCommands: [],
      variableSchema: {},
      defaultVariables: {},
      migrationPolicy: policy,
    }),
    workflowTemplateDocument({
      key: 'workflow',
      name: 'Workflow',
      enabled: true,
      definition: {},
      migrationPolicy: policy,
    }),
    deploymentTemplateDocument({
      key: 'yaml',
      name: 'YAML',
      enabled: true,
      source: { renderer: 'raw_yaml', files: [] },
      parameterSchema: { type: 'object' },
      defaults: {},
      health: { mode: 'configuration_only', timeoutSeconds: 60 },
      migrationPolicy: policy,
    }),
  ]
  values.forEach((value) => expect(value.spec.migrationPolicy).toEqual(policy))
  const assessment = {
    fromVersion: 1,
    toVersion: 4,
    blocked: true,
    requiresApproval: true,
    reason: '先完成 v2；审批不能跳版',
    steps: [{ version: 2, contentDigest: 'digest2', policy }],
  }
  await act(async () => root.render(<MigrationAssessment value={assessment} />))
  expect(host.textContent).toContain('迁移基线 v1 → v4')
  expect(host.textContent).toContain('v2 · 按相邻版本发布 · 不可逆数据变更')
  expect(host.textContent).toContain(policy.guide)
  expect(planMigrations({ impact: { templateMigration: assessment } })).toEqual([assessment])
  expect(planMigrations(null)).toEqual([])
})
