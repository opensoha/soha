import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import tseslint from 'typescript-eslint'

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
    files: ['*.config.js', '*.config.ts', 'eslint.config.js', 'scripts/**/*.mjs'],
    languageOptions: {
      globals: {
        ...globals.node,
        ...globals.es2022,
      },
    },
  },
)
