const ALLOWED_MENU_COMPONENTS = new Set(['TableRowActionMenu', 'KubernetesResourceActionsMenu'])

const FORBIDDEN_INLINE_ROW_COMPONENTS = new Set([
  'Button',
  'ManagementIconButton',
  'Popconfirm',
  'Space',
])

function getPropertyKeyName(prop) {
  if (!prop || prop.type !== 'Property') return null
  if (prop.key.type === 'Identifier') return prop.key.name
  if (prop.key.type === 'Literal') return String(prop.key.value)
  return null
}

function unwrapExpression(node) {
  let current = node
  while (
    current &&
    (current.type === 'TSAsExpression' ||
      current.type === 'TSTypeAssertion' ||
      current.type === 'TSSatisfiesExpression')
  ) {
    current = current.expression
  }
  return current
}

function getStringValue(node) {
  const unwrapped = unwrapExpression(node)
  if (!unwrapped) return null
  if (unwrapped.type === 'Literal' && typeof unwrapped.value === 'string') {
    return unwrapped.value
  }
  if (unwrapped.type === 'TemplateLiteral') {
    return unwrapped.quasis.map((quasi) => quasi.value.cooked ?? quasi.value.raw).join('')
  }
  return null
}

function isPresetSpread(prop, presetKey) {
  if (!prop || prop.type !== 'SpreadElement') return false
  const arg = unwrapExpression(prop.argument)
  return (
    arg?.type === 'MemberExpression' &&
    !arg.computed &&
    arg.object.type === 'Identifier' &&
    arg.object.name === 'tableColumnPresets' &&
    arg.property.type === 'Identifier' &&
    arg.property.name === presetKey
  )
}

function isInsideTableColumnPresets(node, presetName) {
  const parent = node.parent
  if (!parent || parent.type !== 'Property' || getPropertyKeyName(parent) !== presetName) {
    return false
  }
  const grandParent = parent.parent
  if (!grandParent || grandParent.type !== 'ObjectExpression') return false
  const declarator = unwrapExpression(grandParent.parent)
  if (
    grandParent.parent?.type === 'VariableDeclarator' &&
    grandParent.parent.id?.type === 'Identifier' &&
    grandParent.parent.id.name === 'tableColumnPresets'
  ) {
    return true
  }
  if (
    declarator &&
    grandParent.parent?.parent?.type === 'VariableDeclarator' &&
    grandParent.parent.parent.id?.type === 'Identifier' &&
    grandParent.parent.parent.id.name === 'tableColumnPresets'
  ) {
    return true
  }
  return false
}

const ACTION_TITLES = new Set(['操作', 'Actions', 'Action'])

function isActionTitleNode(node) {
  const direct = getStringValue(node)
  if (direct && ACTION_TITLES.has(direct)) return true
  const unwrapped = unwrapExpression(node)
  if (unwrapped?.type === 'CallExpression') {
    return unwrapped.arguments.some((arg) => {
      const text = getStringValue(arg)
      return Boolean(text && ACTION_TITLES.has(text))
    })
  }
  return false
}

function isActionColumnObject(node) {
  let hasActionMarker = false
  let hasTitleActionMarker = false
  let hasDataIndex = false
  let renderProp = null

  for (const prop of node.properties) {
    if (isPresetSpread(prop, 'action')) {
      hasActionMarker = true
      continue
    }
    const keyName = getPropertyKeyName(prop)
    if (keyName === 'dataIndex') {
      hasDataIndex = true
    } else if (keyName === 'render') {
      const value = unwrapExpression(prop.value)
      if (value?.type === 'ArrowFunctionExpression' || value?.type === 'FunctionExpression') {
        renderProp = prop
      }
    } else if (keyName === 'key' && getStringValue(prop.value) === 'actions') {
      hasActionMarker = true
    } else if (keyName === 'title' && isActionTitleNode(prop.value)) {
      hasTitleActionMarker = true
    } else if (keyName === 'className') {
      const text = getStringValue(prop.value)
      if (text && text.includes('soha-table-actions-column')) {
        hasActionMarker = true
      }
    }
  }

  const effectiveMarker = hasActionMarker || (hasTitleActionMarker && !hasDataIndex)
  return effectiveMarker && renderProp ? renderProp : null
}

function isTaskColumnObject(node) {
  if (isInsideTableColumnPresets(node, 'task')) return true
  for (const prop of node.properties) {
    if (isPresetSpread(prop, 'task')) return true
    const keyName = getPropertyKeyName(prop)
    if (keyName === 'title') {
      const titleText = getStringValue(prop.value)
      if (titleText === '最近任务' || titleText === 'Latest task') return true
    }
  }
  return false
}

export const tableRowActionMenuRule = {
  meta: {
    type: 'problem',
    docs: {
      description:
        'Enforce unified 64px ⋮ overflow menus (<TableRowActionMenu /> / <KubernetesResourceActionsMenu />) for table row actions and keep Latest Task columns non-fixed.',
    },
    schema: [],
  },
  create(context) {
    const activeActionRenders = []

    return {
      JSXAttribute(node) {
        if (node.name?.name !== 'className' || !node.value) return
        const raw =
          node.value.type === 'JSXExpressionContainer'
            ? getStringValue(node.value.expression)
            : getStringValue(node.value)
        if (raw && /\bsoha-row-action-icons\b/.test(raw)) {
          context.report({
            node,
            message:
              'Do not render inline multi-icon row action bars (.soha-row-action-icons); fold row actions into <TableRowActionMenu /> (⋮).',
          })
        }
      },

      ObjectExpression(node) {
        if (isTaskColumnObject(node)) {
          const fixedProp = node.properties.find(
            (prop) =>
              getPropertyKeyName(prop) === 'fixed' && getStringValue(prop.value) === 'right',
          )
          if (fixedProp) {
            context.report({
              node: fixedProp,
              message:
                "Do not set fixed: 'right' on Latest Task columns; keep task status columns scrollable so only the 64px ⋮ actions column pins right.",
            })
          }
        }

        if (isInsideTableColumnPresets(node, 'action')) {
          const widthProp = node.properties.find((prop) => getPropertyKeyName(prop) === 'width')
          const widthVal = unwrapExpression(widthProp?.value)
          if (!widthVal || widthVal.type !== 'Literal' || widthVal.value !== 64) {
            context.report({
              node: widthProp ?? node,
              message: 'tableColumnPresets.action must keep a compact 64px width for the ⋮ menu.',
            })
          }
        }

        const renderProp = isActionColumnObject(node)
        if (!renderProp) return

        const widthProp = node.properties.find((prop) => getPropertyKeyName(prop) === 'width')
        if (widthProp) {
          const widthVal = unwrapExpression(widthProp.value)
          if (
            widthVal?.type === 'Literal' &&
            typeof widthVal.value === 'number' &&
            widthVal.value !== 64
          ) {
            context.report({
              node: widthProp,
              message:
                'Table action columns must use width: 64 (or inherit tableColumnPresets.action) for the unified ⋮ menu.',
            })
          }
        }

        activeActionRenders.push({
          renderNode: renderProp.value,
          hasAllowedMenu: false,
        })
      },

      'ObjectExpression:exit'(node) {
        const renderProp = isActionColumnObject(node)
        if (!renderProp) return
        const state = activeActionRenders.pop()
        if (state && !state.hasAllowedMenu) {
          context.report({
            node: renderProp,
            message:
              'Table action columns must delegate row actions to <TableRowActionMenu /> (or <KubernetesResourceActionsMenu />).',
          })
        }
      },

      JSXOpeningElement(node) {
        if (activeActionRenders.length === 0) return
        if (node.name.type !== 'JSXIdentifier') return
        const tagName = node.name.name
        const current = activeActionRenders[activeActionRenders.length - 1]
        if (ALLOWED_MENU_COMPONENTS.has(tagName)) {
          current.hasAllowedMenu = true
          return
        }
        if (FORBIDDEN_INLINE_ROW_COMPONENTS.has(tagName)) {
          context.report({
            node,
            message: `Do not render inline <${tagName}> inside a table actions column; fold all row actions into <TableRowActionMenu /> (⋮).`,
          })
        }
      },
    }
  },
}
