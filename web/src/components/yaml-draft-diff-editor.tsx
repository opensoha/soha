import { useEffect, useMemo, useState } from 'react'
import Editor, { DiffEditor, useMonaco } from '@monaco-editor/react'
import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Segmented, Space, Tag, Typography } from 'antd'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'
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

type ViewMode = 'diff' | 'edit' | 'preview'

interface YamlDraftDiffEditorProps {
  defaultMode?: ViewMode
  description?: string
  editable?: boolean
  leftLabel?: string
  modified: string
  onChange?: (value: string) => void
  onReset?: () => void
  onSave?: () => void
  original: string
  rightLabel?: string
  saveDisabled?: boolean
  title: string
}

export function YamlDraftDiffEditor({
  defaultMode,
  description,
  editable = true,
  leftLabel,
  modified,
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
  const resolvedDefaultMode: ViewMode = defaultMode ?? (editable ? 'edit' : 'preview')
  const [viewMode, setViewMode] = useState<ViewMode>(resolvedDefaultMode)

  useEffect(() => {
    if (!monaco) return
    ensureMonacoWorkers()
  }, [monaco])

  useEffect(() => {
    setViewMode(resolvedDefaultMode)
  }, [resolvedDefaultMode])

  const editorPath = useMemo(() => `file:///yaml-draft-${editable ? 'editable' : 'readonly'}.yaml`, [editable])
  const diffPaths = useMemo(() => ({
    modified: 'file:///yaml-diff-modified.yaml',
    original: 'file:///yaml-diff-original.yaml',
  }), [])

  const availableModes: Array<{ label: string; value: ViewMode }> = editable
    ? [
        { label: t('yamlDiffEditor.editMode', 'Edit'), value: 'edit' },
        { label: t('yamlDiffEditor.diffMode', 'Diff'), value: 'diff' },
      ]
    : [
        { label: t('yamlDiffEditor.previewMode', 'Preview'), value: 'preview' },
        { label: t('yamlDiffEditor.diffMode', 'Diff'), value: 'diff' },
      ]

  return (
    <Card className="kc-detail-card" style={{ marginTop: 0 }}>
      <div className="kc-terminal-toolbar">
        <Space direction="vertical" size={2}>
          <Text strong>{title}</Text>
          {description ? <Text type="secondary" style={{ fontSize: 12 }}>{description}</Text> : null}
        </Space>
        <Space wrap>
          <Segmented<ViewMode>
            options={availableModes}
            value={viewMode}
            onChange={(value) => setViewMode(value as ViewMode)}
          />
          {onReset ? <Button icon={<ReloadOutlined />} onClick={onReset}>{t('common.reset', 'Reset')}</Button> : null}
          {onSave ? <Button onClick={onSave} disabled={saveDisabled}>{t('yamlEditor.saveDraft', 'Save Draft')}</Button> : null}
        </Space>
      </div>

      {viewMode === 'diff' ? (
        <>
          <div className="kc-tag-list" style={{ marginBottom: 12, marginTop: 0 }}>
            <Tag color="default">{leftLabel || t('yamlDiffEditor.originalLabel', 'Original')}</Tag>
            <Tag color="blue">{rightLabel || t('yamlDiffEditor.modifiedLabel', 'Modified')}</Tag>
          </div>
          <DiffEditor
            height="620px"
            language="yaml"
            original={original}
            modified={modified}
            originalModelPath={diffPaths.original}
            modifiedModelPath={diffPaths.modified}
            options={{
              automaticLayout: true,
              readOnly: true,
              originalEditable: false,
              renderSideBySide: true,
              minimap: { enabled: false },
              wordWrap: 'on',
              scrollBeyondLastLine: false,
            }}
          />
        </>
      ) : (
        <Editor
          height="620px"
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
            readOnly: !editable || viewMode === 'preview',
          }}
        />
      )}
    </Card>
  )
}
