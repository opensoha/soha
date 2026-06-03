import { useEffect, useMemo } from 'react'
import Editor, { DiffEditor, useMonaco } from '@monaco-editor/react'
import { CloudUploadOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Space, Tag, Typography } from 'antd'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'
import './resource-operation-panels.css'
import { useI18n } from '@/i18n'

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

export function YamlDraftDiffEditor({
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

  useEffect(() => {
    if (!monaco) return
    ensureMonacoWorkers()
  }, [monaco])

  const editorPath = useMemo(() => 'file:///helm-values-draft.yaml', [])
  const diffPaths = useMemo(() => ({
    modified: 'file:///helm-values-runtime.yaml',
    original: 'file:///helm-values-draft-for-diff.yaml',
  }), [])
  const changed = modified !== original

  return (
    <Card className="soha-detail-card soha-yaml-card" style={{ marginTop: 0 }}>
      <div className="soha-terminal-toolbar soha-yaml-toolbar">
        <Space className="soha-yaml-toolbar-meta" orientation="vertical" size={2}>
          <Text strong>{title}</Text>
          {description ? <Text type="secondary" style={{ fontSize: 12 }}>{description}</Text> : null}
        </Space>
        <Space className="soha-yaml-toolbar-actions" wrap>
          <Tag color={changed ? 'blue' : 'default'}>
            {changed ? t('yamlDiffEditor.changedLabel', 'Changed') : t('yamlDiffEditor.unchangedLabel', 'No changes')}
          </Tag>
          {onReset ? <Button icon={<ReloadOutlined />} onClick={onReset}>{t('common.reset', 'Reset')}</Button> : null}
          {onSave ? <Button onClick={onSave} disabled={saveDisabled}>{t('yamlEditor.saveDraft', 'Save Draft')}</Button> : null}
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
                {editable ? t('yamlDiffEditor.editableLabel', 'Editable') : t('yamlDiffEditor.readOnlyLabel', 'Read-only')}
              </Tag>
            </Space>
          </div>
          <div className="soha-yaml-pane-body">
            <Editor
              height="100%"
              defaultLanguage="yaml"
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
              <Text strong>{rightLabel || t('yamlDiffEditor.runtimeLabel', 'Helm runtime values')}</Text>
              <Tag color="default">{t('yamlDiffEditor.autoDiffLabel', 'Auto diff')}</Tag>
            </Space>
          </div>
          <div className="soha-yaml-pane-body">
            <DiffEditor
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
