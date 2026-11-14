import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import Editor, { DiffEditor, loader, useMonaco } from '@monaco-editor/react'
import type * as Monaco from 'monaco-editor'
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  FullscreenExitOutlined,
  FullscreenOutlined,
  FileSearchOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { Alert, Button, Card, Checkbox, Segmented, Space, Tooltip, Typography, theme } from 'antd'
import { usePreferencesStore } from '@/stores/preferences-store'
import { resolveThemeMode } from '@/theme/app-theme'
import { configureMonacoYaml, type MonacoYaml } from 'monaco-yaml'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import YamlWorker from 'monaco-yaml/yaml.worker?worker'
import './resource-operation-panels.css'
import './k8s-yaml-editor.css'
import { useI18n } from '@/i18n'
import { k8sYamlSchema } from '@/schemas/k8s-yaml-schema'
import { ensureYamlLanguage } from './monaco-yaml-language'
import { releaseOwnedDiffModels } from './yaml-diff-model-lifecycle'
import { yamlChangeReview, yamlValuePreview, type YamlChange } from './yaml-change-review'

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

if (typeof window !== 'undefined') {
  ensureMonacoWorkers()
  loader.config({ monaco: await import('monaco-editor') })
}

export function K8sYamlEditor({
  value,
  original,
  serverChanged,
  onCompareLatest,
  loading,
  error,
  onChange,
  onReset,
  onSave,
  onApply,
  saveDisabled,
  applyDisabled,
  applyDisabledReason,
  applying,
  editorHeight = 'clamp(360px, 64vh, 620px)',
  applyLabel,
  readOnly = false,
  header,
}: {
  value: string
  original?: string
  serverChanged?: boolean
  onCompareLatest?: () => void
  loading?: boolean
  error?: Error | null
  onChange: (value: string) => void
  onReset: () => void
  onSave: () => void
  onApply: () => void
  saveDisabled?: boolean
  applyDisabled?: boolean
  applyDisabledReason?: string
  applying?: boolean
  editorHeight?: number | string
  applyLabel?: ReactNode
  readOnly?: boolean
  header?: ReactNode
}) {
  const { t, localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const monaco = useMonaco()
  const yamlHandleRef = useRef<MonacoYaml | null>(null)
  const id = useId()
  const editorPath = `file:///k8s/${encodeURIComponent(id)}/draft.yaml`
  const standalonePath = `file:///k8s/${encodeURIComponent(id)}/standalone.yaml`
  const originalPath = `file:///k8s/${encodeURIComponent(id)}/original.yaml`
  const [view, setView] = useState<'edit' | 'split' | 'review'>('edit')
  const [onlyChanges, setOnlyChanges] = useState(true)
  const [diff, setDiff] = useState<{
    original: string
    modified: string
    hunks: Monaco.editor.ILineChange[]
  } | null>(null)
  const [selectedKey, setSelectedKey] = useState<string>()
  const [fullscreen, setFullscreen] = useState(false)
  const [fullscreenError, setFullscreenError] = useState('')
  const hostRef = useRef<HTMLDivElement>(null)
  const [diffEditor, setDiffEditor] = useState<Monaco.editor.IStandaloneDiffEditor | null>(null)
  const diffEditorRef = useRef<Monaco.editor.IStandaloneDiffEditor | null>(null)
  const draftEditorRef = useRef<Monaco.editor.IStandaloneCodeEditor | null>(null)
  const ownedModelsRef = useRef<Monaco.editor.IDiffEditorModel | null>(null)
  // Both widgets borrow the compared draft. Detach both before releasing the owned pair;
  // retained models also survive a temporary removal of the comparison widget.
  useLayoutEffect(
    () => () => {
      releaseOwnedDiffModels({
        getModel: () => ownedModelsRef.current,
        setModel: () => {
          if (draftEditorRef.current?.getModel()) draftEditorRef.current.setModel(null)
          if (diffEditorRef.current?.getModel()) diffEditorRef.current.setModel(null)
        },
      })
      ownedModelsRef.current = null
    },
    [],
  )
  const changeRef = useRef(onChange)
  const valueRef = useRef(value)
  changeRef.current = onChange
  valueRef.current = value
  const { token } = theme.useToken()
  const preference = usePreferencesStore((state) => state.themeMode)
  // useToken also rerenders this surface when a system theme changes.
  const dark = resolveThemeMode(preference) === 'dark'
  const editorTheme = dark ? 'soha-yaml-dark' : 'soha-yaml-light'
  const comparing = original !== undefined
  const changed = comparing && value !== original
  const busy = Boolean(loading || applying)

  useEffect(() => {
    if (!monaco) return
    monaco.editor.defineTheme(editorTheme, {
      base: dark ? 'vs-dark' : 'vs',
      inherit: true,
      rules: [],
      colors: {
        'editor.background': token.colorBgContainer,
        'editorGutter.background': token.colorBgContainer,
        // Monaco accepts hex colors; Ant Design's dark background tokens are rgba().
        'diffEditor.insertedLineBackground': `${token.colorSuccess}20`,
        'diffEditor.removedLineBackground': `${token.colorError}20`,
        'diffEditor.insertedTextBackground': `${token.colorSuccess}40`,
        'diffEditor.removedTextBackground': `${token.colorError}40`,
      },
    })
    monaco.editor.setTheme(editorTheme)
  }, [monaco, dark, editorTheme, token.colorBgContainer, token.colorSuccess, token.colorError])
  const hunks = diff?.original === original && diff?.modified === value ? diff.hunks : null
  const changes = useMemo(
    () => (original !== undefined && hunks ? yamlChangeReview(original, value, hunks) : []),
    [original, value, hunks],
  )
  const selected = changes.find((item) => item.key === selectedKey) ?? changes[0]
  const added =
    hunks?.reduce(
      (sum, item) =>
        sum +
        (item.modifiedEndLineNumber
          ? item.modifiedEndLineNumber - item.modifiedStartLineNumber + 1
          : 0),
      0,
    ) ?? 0
  const removed =
    hunks?.reduce(
      (sum, item) =>
        sum +
        (item.originalEndLineNumber
          ? item.originalEndLineNumber - item.originalStartLineNumber + 1
          : 0),
      0,
    ) ?? 0

  useEffect(() => {
    if (!monaco) return
    ensureMonacoWorkers()
    ensureYamlLanguage(monaco)
    const options = {
      enableSchemaRequest: false,
      validate: true,
      completion: true,
      hover: true,
      format: { enable: true },
      yamlVersion: '1.2' as const,
      isKubernetes: true,
      schemas: [
        {
          fileMatch: ['file:///k8s/*/*.yaml'],
          uri: 'inmemory://schema/k8s-resource.json',
          schema: k8sYamlSchema,
        },
      ],
    }
    if (!yamlHandleRef.current) {
      // monaco-yaml uses the legacy worker options supported by Monaco's top-level factory.
      yamlHandleRef.current = configureMonacoYaml(
        {
          ...monaco,
          editor: {
            ...monaco.editor,
            createWebWorker: <T extends object>(
              workerOptions:
                | Parameters<typeof monaco.createWebWorker>[0]
                | Monaco.editor.IInternalWebWorkerOptions,
            ) =>
              'worker' in workerOptions
                ? monaco.editor.createWebWorker<T>(workerOptions)
                : monaco.createWebWorker<T>(workerOptions),
          },
        },
        options,
      )
    } else yamlHandleRef.current.update(options)
  }, [monaco])

  useEffect(() => {
    const updateFullscreen = () => setFullscreen(document.fullscreenElement === hostRef.current)
    document.addEventListener('fullscreenchange', updateFullscreen)
    return () => {
      document.removeEventListener('fullscreenchange', updateFullscreen)
    }
  }, [])

  useEffect(() => {
    if (!diffEditor || !comparing) return
    const countChanges = () => {
      const hunks = diffEditor.getLineChanges()
      setDiff(
        hunks
          ? {
              original: diffEditor.getOriginalEditor().getValue(),
              modified: diffEditor.getModifiedEditor().getValue(),
              hunks,
            }
          : null,
      )
    }
    const subscriptions = [
      diffEditor.onDidUpdateDiff(countChanges),
      diffEditor.getModifiedEditor().onDidChangeModelContent(() => {
        const next = diffEditor.getModifiedEditor().getValue()
        if (next !== valueRef.current) changeRef.current(next)
      }),
    ]
    countChanges()
    return () => subscriptions.forEach((subscription) => subscription.dispose())
  }, [comparing, diffEditor])

  const revealChange = (change: YamlChange) => {
    setSelectedKey(change.key)
    const editor = diffEditor?.getModifiedEditor()
    editor?.setPosition({ lineNumber: change.line, column: 1 })
    editor?.revealLineInCenter(change.line)
  }

  const navigateChange = (direction: 'previous' | 'next') => {
    if (view !== 'review') return diffEditor?.goToDiff(direction)
    const index = changes.indexOf(selected)
    const next =
      changes[(index + (direction === 'next' ? 1 : -1) + changes.length) % changes.length]
    if (next) revealChange(next)
  }

  const changeTitle = (change: YamlChange) => {
    if (!change.path) return zh ? '文本变更' : 'Text change'
    if (zh && change.path.endsWith('.limits.memory')) return '内存限制'
    if (zh && change.path.endsWith('.requests.memory')) return '内存请求'
    const field = change.path.split('.').pop() ?? change.path
    const labels: Record<string, string> = {
      replicas: '副本数量',
      image: '容器镜像',
      memory: '内存',
      cpu: 'CPU',
      command: '启动命令',
      args: '启动参数',
    }
    return zh ? (labels[field] ?? field) : field
  }

  const toggleFullscreen = async () => {
    try {
      setFullscreenError('')
      if (document.fullscreenElement === hostRef.current) await document.exitFullscreen()
      else await hostRef.current?.requestFullscreen()
    } catch {
      setFullscreenError(
        zh
          ? '无法进入全屏，请检查浏览器全屏权限。'
          : 'Fullscreen is unavailable. Check browser permissions.',
      )
    }
  }

  const options: Monaco.editor.IStandaloneEditorConstructionOptions = {
    automaticLayout: true,
    minimap: { enabled: false },
    formatOnPaste: true,
    formatOnType: true,
    // Monaco's asynchronous occurrence highlighter rejects cancellation when these
    // two widgets swap the shared draft model. Keep this configuration editor stable.
    occurrencesHighlight: 'off',
    wordWrap: 'on',
    scrollBeyondLastLine: false,
    tabSize: 2,
    insertSpaces: true,
    readOnly: busy || readOnly,
    ariaLabel: readOnly
      ? zh
        ? '资源 YAML，只读'
        : 'Resource YAML, read-only'
      : zh
        ? 'YAML 草稿编辑器'
        : 'YAML draft editor',
    padding: { top: 12, bottom: 12 },
  }

  return (
    <div
      ref={hostRef}
      className="soha-k8s-yaml-workspace"
      style={{ background: fullscreen ? token.colorBgContainer : undefined }}
    >
      <Card
        className="soha-detail-card soha-yaml-card soha-k8s-yaml-card"
        classNames={{ body: 'soha-k8s-yaml-body' }}
      >
        {header}
        <div className="soha-k8s-yaml-toolbar">
          <Space wrap size={8}>
            {comparing ? (
              <Segmented
                aria-label={zh ? 'YAML 对比方式' : 'YAML comparison view'}
                value={view}
                onChange={setView}
                options={[
                  { value: 'edit', label: zh ? '编辑' : 'Edit' },
                  { value: 'split', label: zh ? '双栏对比' : 'Side by side' },
                  { value: 'review', label: zh ? '变更审阅' : 'Change review' },
                ]}
              />
            ) : (
              <Text strong>
                {readOnly
                  ? zh
                    ? 'YAML · 只读'
                    : 'YAML · Read-only'
                  : zh
                    ? '待创建 YAML'
                    : 'New resource YAML'}
              </Text>
            )}
            {comparing ? (
              <Text type={changed ? undefined : 'secondary'} role="status">
                {changed
                  ? hunks == null
                    ? zh
                      ? '计算差异…'
                      : 'Comparing…'
                    : zh
                      ? `${changes.length} 处变更`
                      : `${changes.length} ${changes.length === 1 ? 'change' : 'changes'}`
                  : zh
                    ? '无变更'
                    : 'No changes'}
              </Text>
            ) : null}
            {changed && hunks ? (
              <span
                className="soha-k8s-yaml-counts"
                aria-label={
                  zh
                    ? `新增 ${added} 行，删除 ${removed} 行`
                    : `${added} lines added, ${removed} lines removed`
                }
              >
                <span className="is-added">+{added}</span>
                <span className="is-removed">−{removed}</span>
              </span>
            ) : null}
          </Space>
          <Space wrap size={8}>
            <Button
              aria-label={
                zh
                  ? fullscreen
                    ? '退出全屏'
                    : '全屏'
                  : fullscreen
                    ? 'Exit fullscreen'
                    : 'Fullscreen'
              }
              icon={fullscreen ? <FullscreenExitOutlined /> : <FullscreenOutlined />}
              onClick={() => void toggleFullscreen()}
            />
            {!readOnly ? (
              <>
                <Button icon={<ReloadOutlined />} onClick={onReset} disabled={busy}>
                  {t('common.reset', 'Reset')}
                </Button>
                <Button onClick={onSave} disabled={saveDisabled || busy}>
                  {t('yamlEditor.saveDraft', 'Save Draft')}
                </Button>
                <Tooltip title={applyDisabledReason}>
                  <span>
                    <Button
                      type="primary"
                      onClick={onApply}
                      loading={applying}
                      disabled={
                        applyDisabled ||
                        busy ||
                        Boolean(error) ||
                        serverChanged ||
                        (comparing && !changed)
                      }
                    >
                      {applyLabel ?? t('common.apply', 'Apply')}
                    </Button>
                  </span>
                </Tooltip>
              </>
            ) : null}
          </Space>
        </div>
        {serverChanged ? (
          <Alert
            showIcon
            type="warning"
            title={
              zh
                ? '集群内容已变化，当前草稿已保留。请先与最新版本重新对比。'
                : 'The cluster resource changed. Your draft is preserved; compare with the latest version before applying.'
            }
            action={
              <Button size="small" disabled={busy} onClick={onCompareLatest}>
                {zh ? '与最新版本对比' : 'Compare latest'}
              </Button>
            }
          />
        ) : null}
        {applyDisabledReason || error || fullscreenError ? (
          <Alert
            showIcon
            type="error"
            title={applyDisabledReason || error?.message || fullscreenError}
          />
        ) : null}
        <div
          className={`soha-k8s-yaml-content ${comparing && view === 'split' ? 'is-split' : ''} ${comparing && view === 'review' ? 'is-review' : ''}`}
          style={{ height: fullscreen ? undefined : editorHeight }}
        >
          {comparing && view === 'review' ? (
            <aside
              className="soha-k8s-yaml-directory"
              aria-label={zh ? '变更目录' : 'Change directory'}
            >
              <div className="soha-k8s-yaml-directory-heading">
                <Text strong>{zh ? '变更目录' : 'Changes'}</Text>
                <Text type="secondary">{changes.length}</Text>
              </div>
              <nav className="soha-k8s-yaml-change-list">
                {changes.map((change, index) => (
                  <button
                    type="button"
                    key={change.key}
                    className="soha-k8s-yaml-change"
                    aria-current={selected?.key === change.key ? 'location' : undefined}
                    onClick={() => revealChange(change)}
                  >
                    <span className="soha-k8s-yaml-change-title">
                      <span>{String(index + 1).padStart(2, '0')}</span>
                      {changeTitle(change)}
                    </span>
                    <code className="soha-k8s-yaml-change-path" title={change.path}>
                      {change.path ??
                        (zh ? `第 ${change.line} 行附近` : `Near line ${change.line}`)}
                    </code>
                    {change.before !== undefined ? (
                      <span className="soha-k8s-yaml-change-value is-removed">
                        <span aria-hidden>−</span>
                        <code>
                          {yamlValuePreview(change.before, change.after) ||
                            (zh ? '空值' : 'Empty value')}
                        </code>
                      </span>
                    ) : null}
                    {change.after !== undefined ? (
                      <span className="soha-k8s-yaml-change-value is-added">
                        <span aria-hidden>+</span>
                        <code>
                          {yamlValuePreview(change.after, change.before) ||
                            (zh ? '空值' : 'Empty value')}
                        </code>
                      </span>
                    ) : null}
                  </button>
                ))}
              </nav>
              {!changes.length ? (
                <div className="soha-k8s-yaml-directory-empty">
                  <FileSearchOutlined />
                  <Text>
                    {changed
                      ? zh
                        ? '正在计算差异'
                        : 'Comparing changes'
                      : zh
                        ? '暂无变更'
                        : 'No changes yet'}
                  </Text>
                  <Text type="secondary">
                    {zh
                      ? '编辑右侧草稿后，在这里查看改动字段与前后值。'
                      : 'Edit the draft to see changed fields and their before/after values here.'}
                  </Text>
                </div>
              ) : null}
            </aside>
          ) : null}
          <div className="soha-k8s-yaml-document">
            {comparing && view !== 'edit' ? (
              <div className="soha-k8s-yaml-diff-tools">
                <Checkbox
                  checked={onlyChanges}
                  onChange={(event) => setOnlyChanges(event.target.checked)}
                >
                  {zh ? '折叠未修改内容' : 'Fold unchanged regions'}
                </Checkbox>
                <Space size={4}>
                  {view === 'review' && selected ? (
                    <Text type="secondary">
                      {changes.indexOf(selected) + 1} / {changes.length}
                    </Text>
                  ) : null}
                  {(['previous', 'next'] as const).map((direction) => (
                    <Button
                      key={direction}
                      type="text"
                      aria-label={
                        direction === 'previous'
                          ? zh
                            ? '上一处'
                            : 'Previous'
                          : zh
                            ? '下一处'
                            : 'Next'
                      }
                      icon={direction === 'previous' ? <ArrowUpOutlined /> : <ArrowDownOutlined />}
                      disabled={!changed || !hunks?.length}
                      onClick={() => navigateChange(direction)}
                    />
                  ))}
                </Space>
              </div>
            ) : null}
            <div className="soha-k8s-yaml-labels">
              {comparing && view !== 'edit' ? (
                <span className="soha-k8s-yaml-original-label">
                  {zh ? '集群基准 · 只读' : 'Cluster baseline · Read-only'}
                </span>
              ) : null}
              <span>
                {readOnly
                  ? zh
                    ? '资源内容 · 只读'
                    : 'Resource content · Read-only'
                  : zh
                    ? '待应用草稿 · 可编辑'
                    : 'Draft · Editable'}
              </span>
            </div>
            <div className="soha-k8s-yaml-editor" aria-busy={loading}>
              {comparing ? (
                <>
                  {/* This component owns the compared models; both widgets retain them across view changes. */}
                  <div className="soha-k8s-yaml-monaco" hidden={view === 'edit'}>
                    <DiffEditor
                      height="100%"
                      language="yaml"
                      theme={editorTheme}
                      original={original}
                      modified={value}
                      originalModelPath={originalPath}
                      modifiedModelPath={editorPath}
                      keepCurrentOriginalModel
                      keepCurrentModifiedModel
                      onMount={(instance) => {
                        diffEditorRef.current = instance
                        ownedModelsRef.current = instance.getModel()
                        setDiffEditor(instance)
                      }}
                      options={{
                        ...options,
                        originalEditable: false,
                        originalAriaLabel: zh ? '集群基准，只读' : 'Cluster baseline, read-only',
                        modifiedAriaLabel: zh ? '待应用 YAML 草稿' : 'YAML draft to apply',
                        renderSideBySide: view === 'split',
                        renderSideBySideInlineBreakpoint: 760,
                        useInlineViewWhenSpaceIsLimited: true,
                        ignoreTrimWhitespace: false,
                        hideUnchangedRegions: {
                          enabled: view !== 'edit' && changed && onlyChanges,
                          contextLineCount: 3,
                          minimumLineCount: 4,
                          revealLineCount: 10,
                        },
                      }}
                    />
                  </div>
                  {view === 'edit' && diffEditor ? (
                    <Editor
                      height="100%"
                      language="yaml"
                      path={editorPath}
                      theme={editorTheme}
                      keepCurrentModel
                      onMount={(instance) => {
                        draftEditorRef.current = instance
                      }}
                      options={options}
                    />
                  ) : null}
                </>
              ) : (
                <Editor
                  height="100%"
                  language="yaml"
                  path={standalonePath}
                  theme={editorTheme}
                  value={value}
                  onChange={(nextValue) => onChange(nextValue ?? '')}
                  options={options}
                />
              )}
            </div>
            {comparing ? (
              <div className="soha-k8s-yaml-footer">
                <span>{zh ? '集群基准 ↔ 本地草稿' : 'Cluster baseline ↔ Local draft'}</span>
                <span>
                  {changed
                    ? zh
                      ? '尚未应用'
                      : 'Not applied'
                    : zh
                      ? '与基准一致'
                      : 'Matches baseline'}
                </span>
              </div>
            ) : null}
          </div>
        </div>
      </Card>
    </div>
  )
}
