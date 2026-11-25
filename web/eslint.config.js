import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import tseslint from 'typescript-eslint'

const webRoot = dirname(fileURLToPath(import.meta.url))
const definedCssClasses = (() => {
  const classes = new Set()
  const walkCss = (dir) => {
    for (const entry of readdirSync(dir)) {
      const fullPath = join(dir, entry)
      if (statSync(fullPath).isDirectory()) walkCss(fullPath)
      else if (entry.endsWith('.css')) {
        for (const match of readFileSync(fullPath, 'utf8').matchAll(/\.([a-zA-Z0-9_-]+)/g)) {
          classes.add(match[1])
        }
      }
    }
  }
  walkCss(join(webRoot, 'src'))
  return classes
})()

const utilityClassPattern =
  /^(?:(?:sm|md|lg|xl|2xl|hover|focus|active|disabled|dark|first|last):)+|.*\[|^(?:grid|inline-grid|inline-flex|flex-(?:row|col|wrap|nowrap|1|auto|initial|none)|items-(?:start|end|center|baseline|stretch)|justify-(?:start|end|center|between|around|evenly)|grid-cols-\S+|col-span-\S+|gap(?:-[xy])?-\S+|space-[xy]-\S+|p[trblxy]?-\d\S*|m[trblxy]?-\d\S*|w-(?:full|screen|auto|fit|min|max|\d\S*)|h-(?:full|screen|auto|fit|min|max|\d\S*)|min-[wh]-\S+|max-[wh]-\S+|text-(?:xs|sm|base|lg|xl|\dxl|left|center|right)|font-(?:thin|light|normal|medium|semibold|bold)|bg-\S+|border(?:-[trblxy0248]|-solid|-dashed|-none)?|rounded(?:-(?:none|sm|md|lg|xl|2xl|full))?|shadow(?:-(?:none|sm|md|lg|xl))?|overflow-(?:auto|hidden|visible|scroll|[xy]-\S+)|list-(?:none|disc|decimal)|relative|absolute|fixed|sticky)$/

const allowedClassPrefixes = [
  'soha-',
  'ant-',
  'anticon',
  'xterm',
  'react-flow',
  'monaco',
  'is-',
  'has-',
]

const sohaStylePlugin = {
  rules: {
    'no-undefined-utility-class': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Disallow uncompiled Tailwind/atomic utility classes that have no matching CSS rule in src/**/*.css.',
        },
        schema: [],
      },
      create(context) {
        function checkClassString(node, rawValue) {
          const invalid = rawValue
            .split(/\s+/)
            .filter(Boolean)
            .filter(
              (token) =>
                !definedCssClasses.has(token) &&
                !allowedClassPrefixes.some((prefix) => token.startsWith(prefix)) &&
                utilityClassPattern.test(token),
            )
          if (invalid.length > 0) {
            context.report({
              node,
              message: `Undefined utility class(es): ${invalid.join(', ')}. Tailwind is not compiled in soha/web; use semantic .soha-* classes, Ant Design layout components, or CSS rules defined in src/styles/.`,
            })
          }
        }

        return {
          JSXAttribute(node) {
            if (
              node.name?.type !== 'JSXIdentifier' ||
              node.name.name !== 'className' ||
              !node.value
            ) {
              return
            }
            if (node.value.type === 'Literal' && typeof node.value.value === 'string') {
              checkClassString(node.value, node.value.value)
            } else if (
              node.value.type === 'JSXExpressionContainer' &&
              node.value.expression.type === 'Literal' &&
              typeof node.value.expression.value === 'string'
            ) {
              checkClassString(node.value.expression, node.value.expression.value)
            }
          },
        }
      },
    },
  },
}

const sohaUiPlugin = {
  rules: {
    'restrained-explanatory-copy': {
      meta: {
        type: 'problem',
        docs: {
          description:
            'Keep page/section titles and action menus restrained; present optional inline explanations via a ? (QuestionCircleOutlined) trigger.',
        },
        schema: [],
      },
      create(context) {
        const DECO_CLASS_RE =
          /\bsoha-[a-z0-9-]*(?:masthead|subaccent|page-subtitle|page-description|editorial-eyebrow)\b/

        function hasHelpOverlayAncestor(node) {
          let current = node.parent
          while (current) {
            if (
              current.type === 'JSXElement' &&
              current.openingElement.name.type === 'JSXIdentifier'
            ) {
              const tag = current.openingElement.name.name
              if (tag === 'Tooltip' || tag === 'Popover') return true
              if (
                tag === 'ManagementIconButton' &&
                current.openingElement.attributes.some(
                  (attr) => attr.type === 'JSXAttribute' && attr.name?.name === 'tooltip',
                )
              ) {
                return true
              }
            }
            current = current.parent
          }
          return false
        }

        return {
          JSXAttribute(node) {
            const attrName = node.name?.name
            if (
              attrName === 'className' &&
              node.value?.type === 'Literal' &&
              typeof node.value.value === 'string' &&
              DECO_CLASS_RE.test(node.value.value)
            ) {
              context.report({
                node,
                message:
                  'Avoid decorative title mastheads or inline subtitle annotations; keep headings restrained and place optional context behind a ? (QuestionCircleOutlined) trigger.',
              })
            }

            if (
              (attrName === 'subtitle' || attrName === 'description') &&
              node.parent?.type === 'JSXOpeningElement' &&
              node.parent.name?.type === 'JSXIdentifier' &&
              /PageHeader$/.test(node.parent.name.name)
            ) {
              context.report({
                node,
                message:
                  'Do not attach inline subtitle/description commentary to page headers; use a ? (QuestionCircleOutlined) Tooltip or Popover when extra explanation is necessary.',
              })
            }
          },

          JSXOpeningElement(node) {
            if (
              node.name.type === 'JSXIdentifier' &&
              node.name.name === 'InfoCircleOutlined' &&
              hasHelpOverlayAncestor(node)
            ) {
              context.report({
                node,
                message:
                  'Use QuestionCircleOutlined (?) instead of InfoCircleOutlined (i) for inline help/explanation Tooltip and Popover triggers.',
              })
            }
          },

          Property(node) {
            const keyName =
              node.key.type === 'Identifier'
                ? node.key.name
                : node.key.type === 'Literal'
                  ? node.key.value
                  : null
            if (
              keyName === 'label' &&
              node.value.type === 'CallExpression' &&
              node.value.callee.type === 'Identifier' &&
              node.value.callee.name === 'actionLabel'
            ) {
              context.report({
                node: node.value,
                message:
                  'Keep enabled menu item labels concise; only wrap with actionLabel(..., reason) inside a disabled/blocked conditional branch.',
              })
            }
          },
        }
      },
    },
  },
}

export default tseslint.config(
  {
    ignores: ['coverage/**', 'dist/**', 'node_modules/**'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.es2022,
      },
    },
    plugins: {
      'react-hooks': reactHooks,
    },
    rules: {
      '@typescript-eslint/no-explicit-any': 'off',
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          argsIgnorePattern: '^_',
          caughtErrorsIgnorePattern: '^_',
          varsIgnorePattern: '^_',
        },
      ],
      'no-undef': 'off',
      'react-hooks/rules-of-hooks': 'error',
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/components/context-feedback.tsx', '**/*.test.*', '**/*.spec.*'],
    plugins: {
      'soha-ui': sohaUiPlugin,
    },
    rules: {
      'soha-ui/restrained-explanatory-copy': 'error',
      'no-restricted-imports': [
        'error',
        {
          paths: [
            'antd',
            'antd/es',
            'antd/es/index',
            'antd/es/index.js',
            'antd/lib',
            'antd/lib/index',
            'antd/lib/index.js',
          ].map((name) => ({
            name,
            importNames: ['Alert'],
            message:
              'Use ContextFeedback for persistent context, ReadFeedback for reads, or OperationFeedback for request notifications.',
          })),
          patterns: ['antd/es/alert', 'antd/es/alert/*', 'antd/lib/alert', 'antd/lib/alert/*'],
        },
      ],
    },
  },
  {
    files: ['src/**/*.tsx'],
    ignores: ['**/*.test.*', '**/*.spec.*', '**/*.stories.*'],
    plugins: {
      'soha-style': sohaStylePlugin,
    },
    rules: {
      'soha-style/no-undefined-utility-class': 'error',
    },
  },
  {
    files: ['*.config.js', '*.config.ts', 'eslint.config.js', 'scripts/**/*.mjs'],
    languageOptions: {
      globals: {
        ...globals.node,
        ...globals.es2022,
      },
    },
  },
)
