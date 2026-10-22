import { lazy, Suspense, useEffect, useId, useMemo, useRef } from 'react'
import Editor, { DiffEditor, loader, useMonaco } from '@monaco-editor/react'
import type { editor as MonacoEditor } from 'monaco-editor'
import { CloudUploadOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Space, Tag, Typography, theme } from 'antd'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'
import './resource-operation-panels.css'
import { useI18n } from '@/i18n'
import { ensureYamlLanguage } from './monaco-yaml-language'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode } from '@/theme/app-theme'

const { Text } = Typography

declare global {
  interface Window {
    MonacoEnvironment?: {
      getWorker?: (_moduleId: string, label: string) => Worker
    }
  }
}

function ensureMonacoWorkers() {
  if (window.MonacoEnvironment?.getWorker) {
    return
  }
  window.MonacoEnvironment = {
    getWorker(_moduleId: string, label: string) {
      switch (label) {
        case 'yaml':
          return new YamlWorker()
        default:
          return new EditorWorker()
      }
    },
  }
}

if (typeof window !== 'undefined') ensureMonacoWorkers()

const LocalYamlDraftDiffEditor = lazy(async () => {
  loader.config({ monaco: await import('monaco-editor') })
  return { default: YamlDraftDiffEditorContent }
})

interface YamlDraftDiffEditorProps {
  description?: string
  editable?: boolean
  applyDisabled?: boolean
  applying?: boolean
  leftLabel?: string
  modified: string
  onApply?: () => void
  onChange?: (value: string) => void
  onReset?: () => void
  onSave?: () => void
  original: string
  rightLabel?: string
  saveDisabled?: boolean
  title: string
}

export function YamlDraftDiffEditor(props: YamlDraftDiffEditorProps) {
  const { t } = useI18n()
  return (
    <Suspense fallback={<div role="status">{t('common.loading', 'Loading...')}</div>}>
      <LocalYamlDraftDiffEditor {...props} />
    </Suspense>
  )
}

function YamlDraftDiffEditorContent({
  description,
  editable = true,
  applyDisabled,
  applying,
  leftLabel,
  modified,
  onApply,
  onChange,
  onReset,
  onSave,
  original,
  rightLabel,
  saveDisabled,
  title,
}: YamlDraftDiffEditorProps) {
  const { t } = useI18n()
  const monaco = useMonaco()
  const { token } = theme.useToken()
  const preference = usePreferencesStore((state) => state.themeMode)
  const editorTheme = resolveThemeMode(preference) === 'dark' ? 'vs-dark' : 'vs'

  useEffect(() => {
    if (!monaco) return
    ensureMonacoWorkers()
    ensureYamlLanguage(monaco)
  }, [monaco])

  const modelScope = useId()
  const modelBase = `file:///soha-yaml/${encodeURIComponent(modelScope)}`
  const editorPath = `${modelBase}/draft.yaml`
  const diffPaths = useMemo(
    () => ({
      modified: `${modelBase}/runtime.yaml`,
      original: `${modelBase}/draft-for-diff.yaml`,
    }),
    [modelBase],
  )
  const diffInstance = useRef<{
    editor: MonacoEditor.IStandaloneDiffEditor
    models: MonacoEditor.IDiffEditorModel | null
  } | null>(null)
  useEffect(
    () => () => {
      const instance = diffInstance.current
      diffInstance.current = null
      if (!instance) return
      // Detach before disposal; each mounted pane owns its model URIs.
      if (instance.editor.getModel()) instance.editor.setModel(null)
      instance.models?.original.dispose()
      instance.models?.modified.dispose()
    },
    [],
  )
  const changed = modified !== original

  return (
    <Card
      className="soha-detail-card soha-yaml-card"
      style={{ marginTop: 0, color: token.colorText }}
    >
      <div className="soha-terminal-toolbar soha-yaml-toolbar">
        <Space className="soha-yaml-toolbar-meta" orientation="vertical" size={2}>
          <Text strong>{title}</Text>
          {description ? (
            <Text type="secondary" style={{ fontSize: 12 }}>
              {description}
            </Text>
          ) : null}
        </Space>
        <Space className="soha-yaml-toolbar-actions" wrap>
          <Tag color={changed ? 'blue' : 'default'}>
            {changed
              ? t('yamlDiffEditor.changedLabel', 'Changed')
              : t('yamlDiffEditor.unchangedLabel', 'No changes')}
          </Tag>
          {onReset ? (
            <Button icon={<ReloadOutlined />} onClick={onReset}>
              {t('common.reset', 'Reset')}
            </Button>
          ) : null}
          {onSave ? (
            <Button onClick={onSave} disabled={saveDisabled}>
              {t('yamlEditor.saveDraft', 'Save Draft')}
            </Button>
          ) : null}
          {onApply ? (
            <Button
              type="primary"
              icon={<CloudUploadOutlined />}
              loading={applying}
              disabled={applyDisabled}
              onClick={onApply}
            >
              {t('common.apply', 'Apply')}
            </Button>
          ) : null}
        </Space>
      </div>

      <div className="soha-yaml-compare-shell">
        <section className="soha-yaml-pane">
          <div className="soha-yaml-pane-header">
            <Space className="soha-yaml-pane-title" size={6} wrap>
              <Text strong>{leftLabel || t('yamlDiffEditor.draftLabel', 'Values draft')}</Text>
              <Tag color={editable ? 'processing' : 'default'}>
                {editable
                  ? t('yamlDiffEditor.editableLabel', 'Editable')
                  : t('yamlDiffEditor.readOnlyLabel', 'Read-only')}
              </Tag>
            </Space>
          </div>
          <div className="soha-yaml-pane-body">
            <Editor
              theme={editorTheme}
              height="100%"
              language="yaml"
              path={editorPath}
              value={modified}
              onChange={(nextValue) => {
                if (!editable || !onChange) return
                onChange(nextValue ?? '')
              }}
              options={{
                automaticLayout: true,
                minimap: { enabled: false },
                wordWrap: 'on',
                scrollBeyondLastLine: false,
                tabSize: 2,
                insertSpaces: true,
                readOnly: !editable,
              }}
            />
          </div>
        </section>

        <section className="soha-yaml-pane">
          <div className="soha-yaml-pane-header">
            <Space className="soha-yaml-pane-title" size={6} wrap>
              <Text strong>
                {rightLabel || t('yamlDiffEditor.runtimeLabel', 'Helm runtime values')}
              </Text>
              <Tag color="default">{t('yamlDiffEditor.autoDiffLabel', 'Auto diff')}</Tag>
            </Space>
          </div>
          <div className="soha-yaml-pane-body">
            <DiffEditor
              keepCurrentOriginalModel
              keepCurrentModifiedModel
              onMount={(editor) => {
                diffInstance.current = { editor, models: editor.getModel() }
              }}
              theme={editorTheme}
              height="100%"
              language="yaml"
              original={modified}
              modified={original}
              originalModelPath={diffPaths.original}
              modifiedModelPath={diffPaths.modified}
              options={{
                automaticLayout: true,
                readOnly: true,
                originalEditable: false,
                renderSideBySide: false,
                minimap: { enabled: false },
                wordWrap: 'on',
                lineNumbers: 'off',
                lineNumbersMinChars: 0,
                glyphMargin: false,
                folding: false,
                scrollBeyondLastLine: false,
              }}
            />
          </div>
        </section>
      </div>
    </Card>
  )
}
