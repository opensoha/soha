import { lazy, Suspense, useEffect, useId, useRef, useState } from 'react'
import Editor, { DiffEditor, loader, useMonaco } from '@monaco-editor/react'
import type * as Monaco from 'monaco-editor'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import JsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'
import { theme } from 'antd'
import { useI18n } from '@/i18n'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode } from '@/theme/app-theme'
import { ensureYamlLanguage } from '@/components/monaco-yaml-language'

const previousWorker = window.MonacoEnvironment?.getWorker
window.MonacoEnvironment = {
  ...window.MonacoEnvironment,
  getWorker: (id, label) =>
    label === 'json'
      ? new JsonWorker()
      : (previousWorker?.(id, label) ?? (label === 'yaml' ? new YamlWorker() : new EditorWorker())),
}

interface ValueEditorProps {
  name: string
  value: string
  original?: string
  comparing: boolean
  readOnly: boolean
  onChange: (value: string) => void
}

const LocalEditor = lazy(async () => {
  loader.config({ monaco: await import('monaco-editor') })
  return { default: ValueEditor }
})

export default function ConfigurationValueEditor(props: ValueEditorProps) {
  const { localeCode } = useI18n()
  return (
    <Suspense
      fallback={
        <div role="status">{localeCode === 'zh_CN' ? '加载编辑器…' : 'Loading editor…'}</div>
      }
    >
      <LocalEditor {...props} />
    </Suspense>
  )
}

function ValueEditor({ name, value, original, comparing, readOnly, onChange }: ValueEditorProps) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const id = useId()
  const path = `inmemory://configuration/${encodeURIComponent(id)}/draft`
  const [diff, setDiff] = useState<Monaco.editor.IStandaloneDiffEditor | null>(null)
  const latest = useRef({ value, onChange, readOnly })
  latest.current = { value, onChange, readOnly }
  const monaco = useMonaco()
  const { token } = theme.useToken()
  const preference = usePreferencesStore((state) => state.themeMode)
  const dark = resolveThemeMode(preference) === 'dark'
  const editorTheme = dark ? 'soha-config-dark' : 'soha-config-light'
  const language = name.endsWith('.json')
    ? 'json'
    : name.endsWith('.yaml') || name.endsWith('.yml')
      ? 'yaml'
      : 'plaintext'

  useEffect(() => {
    if (!monaco) return
    ensureYamlLanguage(monaco)
    monaco.editor.defineTheme(editorTheme, {
      base: dark ? 'vs-dark' : 'vs',
      inherit: true,
      rules: [],
      colors: {
        'editor.background': token.colorBgContainer,
        'editorGutter.background': token.colorBgContainer,
        'diffEditor.insertedLineBackground': `${token.colorSuccess}20`,
        'diffEditor.removedLineBackground': `${token.colorError}20`,
      },
    })
    monaco.editor.setTheme(editorTheme)
  }, [monaco, dark, editorTheme, token.colorBgContainer, token.colorSuccess, token.colorError])

  useEffect(() => {
    if (!diff) return
    const subscription = diff.getModifiedEditor().onDidChangeModelContent(() => {
      const next = diff.getModifiedEditor().getValue()
      if (!latest.current.readOnly && next !== latest.current.value) latest.current.onChange(next)
    })
    return () => subscription.dispose()
  }, [diff])

  const options: Monaco.editor.IStandaloneEditorConstructionOptions = {
    automaticLayout: true,
    minimap: { enabled: false },
    wordWrap: 'on',
    scrollBeyondLastLine: false,
    tabSize: 2,
    readOnly,
    padding: { top: 16, bottom: 16 },
    ariaLabel: zh ? '配置内容编辑器' : 'Configuration content editor',
  }
  if (original === undefined)
    return (
      <Editor
        height="100%"
        value={value}
        language={language}
        theme={editorTheme}
        options={options}
      />
    )
  return (
    <>
      <div className="soha-configuration-data-monaco" hidden={!comparing}>
        <DiffEditor
          height="100%"
          original={original}
          modified={value}
          language={language}
          theme={editorTheme}
          originalModelPath={`${path}/original`}
          modifiedModelPath={path}
          onMount={setDiff}
          options={{
            ...options,
            originalEditable: false,
            renderSideBySide: true,
            renderSideBySideInlineBreakpoint: 640,
            useInlineViewWhenSpaceIsLimited: true,
            ignoreTrimWhitespace: false,
            diffWordWrap: 'on',
            originalAriaLabel: zh ? '修改前，只读' : 'Before, read-only',
            modifiedAriaLabel: zh ? '修改后，草稿' : 'After, draft',
          }}
        />
      </div>
      {!comparing && diff ? (
        <Editor
          height="100%"
          path={path}
          keepCurrentModel
          language={language}
          theme={editorTheme}
          options={options}
        />
      ) : null}
    </>
  )
}
