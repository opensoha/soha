import { useEffect, useMemo, useRef } from 'react'
import Editor, { useMonaco } from '@monaco-editor/react'
import { ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Space, Typography } from 'antd'
import { configureMonacoYaml, type MonacoYaml } from 'monaco-yaml'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'
import './resource-operation-panels.css'
import { useI18n } from '@/i18n'
import { k8sYamlSchema } from '@/schemas/k8s-yaml-schema'

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

export function K8sYamlEditor({
  value,
  onChange,
  onReset,
  onSave,
  onApply,
  saveDisabled,
  applyDisabled,
  applying,
  editorHeight = 620,
}: {
  value: string
  onChange: (value: string) => void
  onReset: () => void
  onSave: () => void
  onApply: () => void
  saveDisabled?: boolean
  applyDisabled?: boolean
  applying?: boolean
  editorHeight?: number | string
}) {
  const { t } = useI18n()
  const monaco = useMonaco()
  const yamlHandleRef = useRef<MonacoYaml | null>(null)

  useEffect(() => {
    if (!monaco) return
    ensureMonacoWorkers()
    if (!yamlHandleRef.current) {
      yamlHandleRef.current = configureMonacoYaml(monaco, {
        enableSchemaRequest: false,
        validate: true,
        completion: true,
        hover: true,
        format: { enable: true },
        yamlVersion: '1.2',
        isKubernetes: true,
        schemas: [
          {
            fileMatch: ['file:///k8s-resource.yaml'],
            uri: 'inmemory://schema/k8s-resource.json',
            schema: k8sYamlSchema,
          },
        ],
      })
      return
    }
    yamlHandleRef.current.update({
      enableSchemaRequest: false,
      validate: true,
      completion: true,
      hover: true,
      format: { enable: true },
      yamlVersion: '1.2',
      isKubernetes: true,
      schemas: [
        {
          fileMatch: ['file:///k8s-resource.yaml'],
          uri: 'inmemory://schema/k8s-resource.json',
          schema: k8sYamlSchema,
        },
      ],
    })
  }, [monaco])

  const editorPath = useMemo(() => 'file:///k8s-resource.yaml', [])

  return (
    <Card className="soha-detail-card soha-yaml-card">
      <div className="soha-terminal-toolbar soha-yaml-toolbar">
        <Space className="soha-yaml-toolbar-meta" orientation="vertical" size={2}>
          <Text strong>{t('yamlEditor.title', 'Kubernetes YAML Editor')}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>{t('yamlEditor.hint', 'Monaco + monaco-yaml with local schema assistance enabled')}</Text>
        </Space>
        <Space className="soha-yaml-toolbar-actions" wrap>
          <Button variant="outlined" icon={<ReloadOutlined />} onClick={onReset}>{t('common.reset', 'Reset')}</Button>
          <Button variant="outlined" onClick={onSave} disabled={saveDisabled}>{t('yamlEditor.saveDraft', 'Save Draft')}</Button>
          <Button type="primary" onClick={onApply} loading={applying} disabled={applyDisabled}>{t('common.apply', 'Apply')}</Button>
        </Space>
      </div>
      <div className="soha-yaml-editor-shell" style={{ height: editorHeight }}>
        <Editor
          height="100%"
          defaultLanguage="yaml"
          path={editorPath}
          value={value}
          onChange={(nextValue) => onChange(nextValue ?? '')}
          options={{
            automaticLayout: true,
            minimap: { enabled: false },
            formatOnPaste: true,
            formatOnType: true,
            wordWrap: 'on',
            scrollBeyondLastLine: false,
            tabSize: 2,
            insertSpaces: true,
          }}
        />
      </div>
    </Card>
  )
}
