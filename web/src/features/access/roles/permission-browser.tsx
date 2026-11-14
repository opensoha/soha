import { useMemo, useState } from 'react'
import type { PermissionDefinition } from '@opensoha/contracts/gen/ts/sohaapi'
import { Alert, Button, Checkbox, Empty, Spin, Tree } from 'antd'
import { LeftOutlined, ReloadOutlined, RightOutlined } from '@ant-design/icons'
import type { DataNode } from 'antd/es/tree'
import { useI18n } from '@/i18n'
import { buildRolePermissionTreeData } from './permission-model'

type BrowserAction = {
  disabled: boolean
  key: string
  label: string
  value: string
}

export type PermissionBrowserNode = {
  key: string
  title: string
  actions: BrowserAction[]
  children: PermissionBrowserNode[]
}

type PermissionBrowserData = {
  workbenches: PermissionBrowserNode[]
}

type RolePermissionBrowserProps = {
  definitions: PermissionDefinition[]
  error?: boolean
  loading?: boolean
  permissionKeys: string[]
  onChange: (permissionKeys: string[]) => void
  onRetry?: () => void
}

function nodeTitle(node: DataNode) {
  return typeof node.title === 'string' ? node.title : String(node.title ?? '')
}

function actionFromNode(node: DataNode): BrowserAction {
  const key = String(node.key)
  return {
    disabled: Boolean(node.disabled),
    key,
    label: nodeTitle(node),
    value: key.slice('permission:'.length),
  }
}

function browserNodeFromTree(node: DataNode): PermissionBrowserNode {
  const children = (node.children ?? []) as DataNode[]
  return {
    key: String(node.key),
    title: nodeTitle(node),
    actions: children
      .filter((child) => String(child.key).startsWith('permission:'))
      .map(actionFromNode),
    children: children
      .filter((child) => !String(child.key).startsWith('permission:'))
      .map(browserNodeFromTree)
      .filter((child) => permissionValuesForNode(child).length > 0),
  }
}

export function buildRolePermissionBrowserData(treeData: DataNode[]): PermissionBrowserData {
  const workbenches = treeData
    .filter((node) => String(node.key).startsWith('workbench:'))
    .map((node) => {
      const workbench = browserNodeFromTree(node)
      if (!workbench.actions.length) return workbench
      const resourceCreationActions =
        workbench.key === 'workbench:platform'
          ? workbench.actions.filter((action) => action.value === 'platform.resource-creation.use')
          : []
      const entryActions = workbench.actions.filter((action) =>
        action.value.startsWith('workbench.'),
      )
      const compatibilityActions =
        workbench.key === 'workbench:compatibility' ? workbench.actions : []
      const pageActions =
        workbench.key === 'workbench:compatibility'
          ? []
          : workbench.actions.filter(
              (action) =>
                !action.value.startsWith('workbench.') &&
                action.value !== 'platform.resource-creation.use',
            )
      return {
        ...workbench,
        actions: [],
        children: [
          ...(entryActions.length
            ? [
                {
                  key: `entry:${workbench.key}`,
                  title: '工作台入口',
                  actions: entryActions,
                  children: [],
                },
              ]
            : []),
          ...(compatibilityActions.length
            ? [
                {
                  key: `compatibility:${workbench.key}`,
                  title: '保留项',
                  actions: compatibilityActions,
                  children: [],
                },
              ]
            : []),
          ...(resourceCreationActions.length
            ? [
                {
                  key: 'capability:platform-resource-creation',
                  title: '资源创建',
                  actions: resourceCreationActions,
                  children: [],
                },
              ]
            : []),
          ...(pageActions.length
            ? [
                {
                  key: `pages:${workbench.key}`,
                  title: '页面权限',
                  actions: pageActions,
                  children: [],
                },
              ]
            : []),
          ...workbench.children,
        ],
      }
    })
  return { workbenches }
}

export function permissionValuesForNode(node: PermissionBrowserNode): string[] {
  return [
    ...node.actions.map((action) => action.value),
    ...node.children.flatMap(permissionValuesForNode),
  ]
}

function editablePermissionValuesForNode(node: PermissionBrowserNode): string[] {
  return [
    ...node.actions.filter((action) => !action.disabled).map((action) => action.value),
    ...node.children.flatMap(editablePermissionValuesForNode),
  ]
}

export function toggleRolePermissionValues(
  currentValues: string[],
  targetValues: string[],
  checked: boolean,
) {
  const next = new Set(currentValues)
  targetValues.forEach((value) => (checked ? next.add(value) : next.delete(value)))
  return [...next].sort((left, right) => left.localeCompare(right))
}

function selectionState(currentValues: string[], targetValues: string[]) {
  const current = new Set(currentValues)
  const selected = targetValues.filter((value) => current.has(value)).length
  return {
    checked: targetValues.length > 0 && selected === targetValues.length,
    indeterminate: selected > 0 && selected < targetValues.length,
    selected,
    total: targetValues.length,
  }
}

function flattenMenuNodes(nodes: PermissionBrowserNode[]): PermissionBrowserNode[] {
  return nodes.flatMap((node) => [node, ...flattenMenuNodes(node.children)])
}

function firstActionableNode(nodes: PermissionBrowserNode[]): PermissionBrowserNode | undefined {
  for (const node of nodes) {
    if (node.actions.length) return node
    const child = firstActionableNode(node.children)
    if (child) return child
  }
  return nodes[0]
}

function menuTreeData(nodes: PermissionBrowserNode[]): DataNode[] {
  return nodes.map((node) => ({
    key: node.key,
    title: node.title,
    disabled: editablePermissionValuesForNode(node).length === 0,
    children: menuTreeData(node.children),
  }))
}

export function RolePermissionBrowser({
  definitions,
  error,
  loading,
  permissionKeys,
  onChange,
  onRetry,
}: RolePermissionBrowserProps) {
  const { localeCode, t } = useI18n()
  const { workbenches } = useMemo(() => {
    const data = buildRolePermissionBrowserData(
      buildRolePermissionTreeData(definitions, permissionKeys, t),
    )
    const syntheticTitles: Record<string, string> = {
      工作台入口: t('access.roles.workbenchEntry', '工作台入口'),
      保留项: t('access.roles.compatibilityItems', '保留项'),
      资源创建: t('access.roles.resourceCreation', '资源创建'),
      页面权限: t('access.roles.pagePermissions', '页面权限'),
    }
    const localize = (node: PermissionBrowserNode): PermissionBrowserNode => ({
      ...node,
      title: syntheticTitles[node.title] ?? node.title,
      children: node.children.map(localize),
    })
    return { workbenches: data.workbenches.map(localize) }
  }, [definitions, permissionKeys, t])
  const [activeScopeKey, setActiveScopeKey] = useState('')
  const [activeMenuKey, setActiveMenuKey] = useState('')
  const [mobileStage, setMobileStage] = useState<'workbenches' | 'menus' | 'actions'>('workbenches')
  const activeWorkbench =
    workbenches.find((workbench) => workbench.key === activeScopeKey) ?? workbenches[0]
  const menuNodes = activeWorkbench?.children ?? []
  const flatMenuNodes = flattenMenuNodes(menuNodes)
  const activeMenu =
    flatMenuNodes.find((node) => node.key === activeMenuKey) ?? firstActionableNode(menuNodes)
  const menuNodeByKey = new Map(flatMenuNodes.map((node) => [node.key, node]))
  const checkedMenuKeys: string[] = []
  const halfCheckedMenuKeys: string[] = []

  flatMenuNodes.forEach((node) => {
    const state = selectionState(permissionKeys, permissionValuesForNode(node))
    if (state.checked) checkedMenuKeys.push(node.key)
    if (state.indeterminate) halfCheckedMenuKeys.push(node.key)
  })

  const updatePermissionKeys = (targets: string[], checked: boolean) =>
    onChange(toggleRolePermissionValues(permissionKeys, targets, checked))
  const selectWorkbench = (workbench: PermissionBrowserNode) => {
    setActiveScopeKey(workbench.key)
    setActiveMenuKey(firstActionableNode(workbench.children)?.key ?? '')
    setMobileStage('menus')
  }
  const selectMenu = (key: string) => {
    setActiveMenuKey(key)
    setMobileStage('actions')
  }
  const assignableValues = definitions
    .filter((definition) => definition.assignable && definition.status === 'active')
    .map((definition) => definition.key)
  const compatibilityCount = permissionKeys.filter(
    (permissionKey) => !assignableValues.includes(permissionKey),
  ).length

  if (loading) {
    return (
      <div className="soha-role-permission-browser-loading">
        <Spin size="small" />
      </div>
    )
  }
  if (error) {
    return (
      <Alert
        showIcon
        title={t('access.roles.catalogLoadFailed', '权限目录加载失败')}
        description={t('access.roles.catalogLoadFailedDescription', '请重试后再配置角色权限。')}
        type="error"
        action={
          onRetry ? (
            <Button size="small" icon={<ReloadOutlined />} onClick={onRetry}>
              {t('common.retry', '重试')}
            </Button>
          ) : null
        }
      />
    )
  }
  if (!workbenches.length)
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={t('access.roles.catalogEmpty', '权限目录为空')}
      />
    )

  return (
    <div className="soha-role-permission-browser">
      <div className="soha-role-permission-browser-summary">
        <span>
          {t('access.roles.exactPermissions', '精确权限')}{' '}
          {selectionState(permissionKeys, assignableValues).selected}/{assignableValues.length}
        </span>
        {compatibilityCount ? (
          <span>
            {t('access.roles.compatibilityPermissions', '兼容权限')} {compatibilityCount}
            {t('access.roles.readonlySuffix', '（只读）')}
          </span>
        ) : null}
      </div>
      <div className={`soha-role-permission-browser-grid is-stage-${mobileStage}`}>
        <section
          className="soha-role-permission-browser-pane is-workbenches"
          data-permission-level="workbenches"
        >
          <header className="soha-role-permission-browser-pane-header">
            {t('access.roles.workbenches', '工作台')}
          </header>
          <div className="soha-role-permission-browser-pane-body">
            {workbenches.map((workbench) => {
              const targets = editablePermissionValuesForNode(workbench)
              const allValues = permissionValuesForNode(workbench)
              const state = selectionState(permissionKeys, allValues)
              const active = workbench.key === activeWorkbench?.key
              return (
                <div
                  key={workbench.key}
                  className={`soha-role-permission-browser-row${active ? ' is-active' : ''}`}
                >
                  <Checkbox
                    aria-label={
                      localeCode === 'zh_CN'
                        ? `选择${workbench.title}全部权限`
                        : `Select all permissions for ${workbench.title}`
                    }
                    checked={state.checked}
                    disabled={targets.length === 0}
                    indeterminate={state.indeterminate}
                    onChange={(event) => updatePermissionKeys(targets, event.target.checked)}
                  />
                  <button
                    type="button"
                    className="soha-role-permission-browser-row-button"
                    aria-pressed={active}
                    onClick={() => selectWorkbench(workbench)}
                  >
                    <span className="soha-role-permission-browser-row-title">
                      {workbench.title}
                    </span>
                    <span className="soha-role-permission-browser-count">
                      {state.selected}/{state.total}
                    </span>
                    <RightOutlined aria-hidden />
                  </button>
                </div>
              )
            })}
          </div>
        </section>

        <section
          className="soha-role-permission-browser-pane is-menus"
          data-permission-level="menus"
        >
          <header className="soha-role-permission-browser-pane-header">
            <Button
              className="soha-role-permission-browser-back"
              type="text"
              size="small"
              icon={<LeftOutlined />}
              aria-label={t('access.roles.backToWorkbenches', '返回工作台')}
              onClick={() => setMobileStage('workbenches')}
            />
            <span>{activeWorkbench?.title ?? t('access.roles.menusAndPages', '菜单与页面')}</span>
          </header>
          <div className="soha-role-permission-browser-pane-body">
            <Tree
              key={activeWorkbench?.key}
              blockNode
              checkable
              checkStrictly
              defaultExpandedKeys={menuNodes.map((node) => node.key)}
              selectedKeys={activeMenu ? [activeMenu.key] : []}
              showLine={{ showLeafIcon: false }}
              treeData={menuTreeData(menuNodes)}
              checkedKeys={{ checked: checkedMenuKeys, halfChecked: halfCheckedMenuKeys }}
              titleRender={(treeNode) => {
                const node = menuNodeByKey.get(String(treeNode.key))
                const state = node
                  ? selectionState(permissionKeys, permissionValuesForNode(node))
                  : { selected: 0, total: 0 }
                return (
                  <span className="soha-role-permission-browser-tree-title">
                    <span>{node?.title ?? nodeTitle(treeNode)}</span>
                    <span className="soha-role-permission-browser-count">
                      {state.selected}/{state.total}
                    </span>
                  </span>
                )
              }}
              onCheck={(_, info) => {
                const node = menuNodeByKey.get(String(info.node.key))
                if (!node) return
                const targets = editablePermissionValuesForNode(node)
                const state = selectionState(permissionKeys, targets)
                updatePermissionKeys(targets, !state.checked)
              }}
              onSelect={(_, info) => selectMenu(String(info.node.key))}
            />
          </div>
        </section>

        <section
          className="soha-role-permission-browser-pane is-actions"
          data-permission-level="actions"
        >
          <header className="soha-role-permission-browser-pane-header">
            <Button
              className="soha-role-permission-browser-back"
              type="text"
              size="small"
              icon={<LeftOutlined />}
              aria-label={t('access.roles.backToMenus', '返回菜单与页面')}
              onClick={() => setMobileStage('menus')}
            />
            <span>{activeMenu?.title ?? t('access.roles.pageActions', '页面动作')}</span>
            {activeMenu?.actions.some((action) => !action.disabled) ? (
              <Checkbox
                aria-label={t('access.roles.selectCurrentPageAll', '选择当前页面全部权限')}
                checked={
                  selectionState(
                    permissionKeys,
                    activeMenu.actions
                      .filter((action) => !action.disabled)
                      .map((action) => action.value),
                  ).checked
                }
                indeterminate={
                  selectionState(
                    permissionKeys,
                    activeMenu.actions
                      .filter((action) => !action.disabled)
                      .map((action) => action.value),
                  ).indeterminate
                }
                onChange={(event) =>
                  updatePermissionKeys(
                    activeMenu.actions
                      .filter((action) => !action.disabled)
                      .map((action) => action.value),
                    event.target.checked,
                  )
                }
              >
                {t('common.selectAll', '全选')}
              </Checkbox>
            ) : null}
          </header>
          <div className="soha-role-permission-browser-pane-body is-actions-list">
            {activeMenu?.actions.length ? (
              activeMenu.actions.map((action) => (
                <Checkbox
                  key={action.key}
                  checked={permissionKeys.includes(action.value)}
                  disabled={action.disabled}
                  onChange={(event) => updatePermissionKeys([action.value], event.target.checked)}
                >
                  {action.label}
                </Checkbox>
              ))
            ) : (
              <div className="soha-role-permission-browser-empty">
                {t('access.roles.noDirectPermissions', '此菜单没有直接功能权限')}
              </div>
            )}
          </div>
        </section>
      </div>
    </div>
  )
}
