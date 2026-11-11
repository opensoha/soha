import { ManagementState } from '@/components/management-list'
import { useI18n } from '@/i18n'
import {
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  EyeInvisibleOutlined,
  EyeOutlined,
  FileTextOutlined,
  PlusOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import { Alert, App, Button, Card, Input, Segmented, Spin } from 'antd'
import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import {
  configurationDataRows,
  configurationDataSize,
  copyConfigurationValue,
} from './data-primitives'
import './data-workspace.css'

const ConfigurationValueEditor = lazy(() => import('./value-editor'))

interface DataEntry {
  id: string
  key: string
  value: string
  originalKey?: string
  binary?: boolean
  deleted?: boolean
}

function dataEntries(detail: {
  data?: Record<string, string>
  binaryData?: Record<string, string>
  immutable: boolean
}): DataEntry[] {
  return [
    ...configurationDataRows(detail.data).map((entry) => ({
      ...entry,
      id: `data/${entry.key}`,
      originalKey: entry.key,
    })),
    ...configurationDataRows(detail.binaryData).map((entry) => ({
      ...entry,
      id: `binary/${entry.key}`,
      binary: true,
    })),
  ]
}

export function ConfigurationDataWorkspace({
  applying,
  blocked,
  canEdit,
  detail,
  encodedData,
  readOnlyReason,
  onApply,
  searchable = false,
  editRequest = 0,
  active = true,
}: {
  active?: boolean
  searchable?: boolean
  editRequest?: number
  blocked?: boolean
  applying?: boolean
  canEdit: boolean
  detail: { data?: Record<string, string>; binaryData?: Record<string, string>; immutable: boolean }
  encodedData?: Record<string, string>
  readOnlyReason?: string
  onApply: (data: Record<string, string>) => void | Promise<unknown>
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { message } = App.useApp()
  const [edit, setEdit] = useState<{
    baseline: string
    original: Record<string, string>
    entries: DataEntry[]
  } | null>(null)
  const [selection, setSelection] = useState<string>()
  const [view, setView] = useState<'edit' | 'diff'>('edit')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [encoding, setEncoding] = useState<'text' | 'base64'>('text')
  const [revealedKey, setRevealedKey] = useState<string>()
  const nextId = useRef(0)
  const handledEdit = useRef(0)
  const [keyword, setKeyword] = useState('')
  const entries = edit?.entries ?? dataEntries(detail)
  const selected = entries.find((entry) => entry.id === selection) ?? entries[0]
  const secret = encodedData !== undefined
  const visible = !secret || Boolean(edit) || (active && revealedKey === selected?.id)
  const baseline = JSON.stringify([detail.data ?? {}, detail.binaryData ?? {}, encodedData])
  const conflict = Boolean(edit && edit.baseline !== baseline)
  const permitted = canEdit && !detail.immutable
  const busy = Boolean(applying || saving || blocked)
  useEffect(() => {
    if (!active) setRevealedKey(undefined)
  }, [active])
  useEffect(() => {
    if (!editRequest || editRequest === handledEdit.current) return
    handledEdit.current = editRequest
    if (!permitted || busy || edit) return
    setEdit({ baseline, original: detail.data ?? {}, entries: dataEntries(detail) })
    setView('edit')
    setError('')
  }, [editRequest, permitted, busy, edit, baseline, detail])
  const filteredEntries =
    searchable && keyword.trim()
      ? entries.filter((entry) => entry.key.toLowerCase().includes(keyword.trim().toLowerCase()))
      : entries
  const originalValue = (entry: DataEntry) => edit?.original[entry.originalKey ?? ''] ?? ''
  const entryChanged = (entry: DataEntry) =>
    !entry.binary &&
    (entry.deleted
      ? entry.originalKey !== undefined
      : entry.originalKey === undefined ||
        entry.key.trim() !== entry.originalKey ||
        entry.value !== originalValue(entry))
  const changedCount = edit ? entries.filter(entryChanged).length : 0
  const editable = Boolean(
    edit && permitted && !busy && selected && !selected.binary && !selected.deleted,
  )
  const displayedValue = selected
    ? selected.deleted
      ? ''
      : secret && !edit && encoding === 'base64'
        ? (encodedData?.[selected.key] ?? '')
        : selected.value
    : ''
  const updateSelected = (change: Partial<DataEntry>) => {
    if (!editable) return
    setError('')
    setEdit((current) =>
      current
        ? {
            ...current,
            entries: current.entries.map((entry) =>
              entry.id === selected?.id ? { ...entry, ...change } : entry,
            ),
          }
        : current,
    )
  }
  const apply = async () => {
    if (!edit || !permitted || busy || conflict || !changedCount) return
    const active = edit.entries.filter((entry) => !entry.binary && !entry.deleted)
    const keys = active.map((entry) => entry.key.trim())
    const duplicate = keys.find(
      (key, index) =>
        keys.indexOf(key) !== index ||
        Object.prototype.hasOwnProperty.call(detail.binaryData ?? {}, key),
    )
    if (keys.some((key) => !key || !/^[A-Za-z0-9._-]+$/.test(key))) {
      setError(
        zh
          ? 'Key 不能为空，只能包含字母、数字、点、短横线或下划线。'
          : 'Keys must contain only letters, numbers, dots, hyphens or underscores.',
      )
      return
    }
    if (duplicate) {
      setError(zh ? `Key 重复：${duplicate}` : `Duplicate key: ${duplicate}`)
      return
    }
    setError('')
    setSaving(true)
    try {
      await onApply(Object.fromEntries(active.map((entry, index) => [keys[index], entry.value])))
      setEdit(null)
      setView('edit')
    } catch (failure) {
      setError(
        failure instanceof Error
          ? failure.message
          : zh
            ? '应用失败，草稿已保留。'
            : 'Apply failed. Your draft is preserved.',
      )
    } finally {
      setSaving(false)
    }
  }

  const editControls = (
    <div className="soha-configuration-data-actions">
      {edit ? (
        <>
          <Button
            disabled={busy}
            onClick={() => {
              setEdit(null)
              setError('')
              setView('edit')
              setEncoding('text')
            }}
          >
            {zh ? '取消编辑' : 'Cancel editing'}
          </Button>
          <Button
            type="primary"
            disabled={!permitted || conflict || !changedCount || busy}
            loading={busy}
            onClick={() => void apply()}
          >
            {zh ? '应用更改' : 'Apply changes'}
          </Button>
        </>
      ) : (
        <Button
          type="primary"
          icon={<EditOutlined />}
          disabled={!permitted || busy}
          onClick={() => {
            setEdit({
              baseline,
              original: detail.data ?? {},
              entries: dataEntries(detail),
            })
            setError('')
            setView('edit')
            setEncoding('text')
            setRevealedKey(undefined)
          }}
        >
          {zh ? '编辑数据' : 'Edit Data'}
        </Button>
      )}
    </div>
  )

  return (
    <div className="soha-configuration-data-layout">
      <Card
        className="soha-detail-card soha-configuration-data-directory-card"
        styles={{ body: { padding: 0 } }}
      >
        <aside
          className="soha-configuration-data-directory"
          aria-label={zh ? '数据键列表' : 'Data keys'}
        >
          <div className="soha-configuration-data-directory-header">
            <div className="soha-configuration-data-identity">
              <strong>Key</strong>{' '}
              <span role="status">
                {edit
                  ? zh
                    ? `${changedCount} 个 Key 待应用`
                    : `${changedCount} keys to apply`
                  : zh
                    ? `共 ${entries.length} 个键`
                    : `${entries.length} keys`}
              </span>
            </div>
            {edit ? (
              <Button
                aria-label={zh ? '新增数据键' : 'Add data key'}
                type="text"
                icon={<PlusOutlined />}
                disabled={!permitted || busy}
                onClick={() => {
                  const entry = { id: `new/${++nextId.current}`, key: '', value: '' }
                  setEdit((current) =>
                    current ? { ...current, entries: [...current.entries, entry] } : current,
                  )
                  setSelection(entry.id)
                  setKeyword('')
                  setView('edit')
                }}
              />
            ) : null}
          </div>
          {searchable && (
            <div className="soha-configuration-data-search">
              <Input
                allowClear
                aria-label={zh ? '搜索数据键' : 'Search data keys'}
                placeholder={zh ? '搜索 Key' : 'Search keys'}
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
              />
            </div>
          )}
          <nav className="soha-configuration-data-keys">
            {filteredEntries.map((entry) => (
              <button
                type="button"
                key={entry.id}
                aria-current={selected?.id === entry.id ? 'true' : undefined}
                className={`soha-configuration-data-key ${entry.deleted ? 'is-deleted' : ''}`}
                onClick={() => {
                  setSelection(entry.id)
                  setRevealedKey(undefined)
                  if (entry.binary) setView('edit')
                }}
              >
                <FileTextOutlined />
                <span className="soha-configuration-data-key-content">
                  <span title={entry.key}>{entry.key || (zh ? '未命名 Key' : 'Unnamed key')}</span>
                  <small>
                    {entry.binary
                      ? secret
                        ? zh
                          ? '二进制 · Base64'
                          : 'Binary · Base64'
                        : 'binaryData · Base64'
                      : `${configurationDataSize(entry.value)} · ${entry.value.split('\n').length} ${zh ? '行' : 'lines'}`}
                  </small>
                </span>
                {edit && entryChanged(entry) ? (
                  <span className="soha-configuration-data-key-status">
                    {entry.deleted
                      ? zh
                        ? '待删除'
                        : 'Deleted'
                      : entry.originalKey === undefined
                        ? zh
                          ? '新增'
                          : 'New'
                        : zh
                          ? '已修改'
                          : 'Changed'}
                  </span>
                ) : null}
              </button>
            ))}
          </nav>
          {!filteredEntries.length ? (
            <div className="soha-configuration-data-directory-empty">
              {entries.length
                ? zh
                  ? '没有匹配的 Key'
                  : 'No matching keys'
                : zh
                  ? '暂无数据键'
                  : 'No data keys'}
            </div>
          ) : null}
        </aside>
      </Card>
      <Card
        className="soha-detail-card soha-configuration-data-content-card"
        styles={{ body: { padding: 0 } }}
      >
        {conflict ? (
          <Alert
            showIcon
            type="warning"
            title={
              zh
                ? '集群数据已变化，草稿已保留。请取消编辑后重新读取最新内容。'
                : 'Cluster data changed. Your draft is preserved. Cancel editing to reload the latest content.'
            }
          />
        ) : null}
        {error ? <Alert showIcon type="error" title={error} /> : null}
        {readOnlyReason ? <Alert showIcon type="info" title={readOnlyReason} /> : null}
        <section
          className="soha-configuration-data-document"
          aria-label={zh ? '数据内容' : 'Data content'}
        >
          {selected ? (
            <>
              <div className="soha-configuration-data-document-header">
                <div className="soha-configuration-data-identity">
                  {editable ? (
                    <Input
                      aria-label={zh ? '数据键' : 'Data key'}
                      value={selected.key}
                      onChange={(event) => updateSelected({ key: event.target.value })}
                    />
                  ) : (
                    <strong title={selected.key}>
                      {selected.key || (zh ? '未命名 Key' : 'Unnamed key')}
                    </strong>
                  )}
                  <small>
                    {selected.binary
                      ? zh
                        ? 'Base64 · 只读'
                        : 'Base64 · Read-only'
                      : edit
                        ? zh
                          ? '本地草稿'
                          : 'Local draft'
                        : zh
                          ? secret
                            ? encoding === 'base64'
                              ? 'Base64 · 只读'
                              : '解码内容 · 只读'
                            : '集群内容 · 只读'
                          : secret
                            ? encoding === 'base64'
                              ? 'Base64 · Read-only'
                              : 'Decoded content · Read-only'
                            : 'Cluster content · Read-only'}
                  </small>
                </div>
                <div className="soha-configuration-data-actions">
                  {secret && !edit && !selected.binary ? (
                    <Segmented<'text' | 'base64'>
                      aria-label={zh ? '内容编码' : 'Content encoding'}
                      value={encoding}
                      options={[
                        { value: 'text', label: zh ? '解码内容' : 'Decoded' },
                        { value: 'base64', label: 'Base64' },
                      ]}
                      onChange={setEncoding}
                    />
                  ) : null}
                  {secret && !edit ? (
                    <Button
                      type="text"
                      aria-label={
                        visible
                          ? zh
                            ? '隐藏内容'
                            : 'Hide content'
                          : zh
                            ? '显示内容'
                            : 'Show content'
                      }
                      icon={visible ? <EyeInvisibleOutlined /> : <EyeOutlined />}
                      onClick={() => setRevealedKey(visible ? undefined : selected.id)}
                    />
                  ) : null}
                  {edit && !selected.binary ? (
                    <Segmented<'edit' | 'diff'>
                      aria-label={zh ? '内容视图' : 'Content view'}
                      value={view}
                      options={[
                        { value: 'edit', label: zh ? '编辑' : 'Edit' },
                        { value: 'diff', label: zh ? '差异对比' : 'Compare changes' },
                      ]}
                      onChange={setView}
                    />
                  ) : null}
                  <Button
                    aria-label={zh ? '复制内容' : 'Copy content'}
                    type="text"
                    icon={<CopyOutlined />}
                    onClick={() => copyConfigurationValue(displayedValue, localeCode, message)}
                  />
                  {edit && !selected.binary ? (
                    <Button
                      danger={!selected.deleted}
                      disabled={!permitted || busy}
                      aria-label={
                        selected.deleted
                          ? zh
                            ? '恢复数据键'
                            : 'Restore key'
                          : zh
                            ? '删除数据键'
                            : 'Delete data key'
                      }
                      type="text"
                      icon={selected.deleted ? <UndoOutlined /> : <DeleteOutlined />}
                      onClick={() =>
                        setEdit((current) =>
                          current
                            ? {
                                ...current,
                                entries: current.entries.map((entry) =>
                                  entry.id === selected.id
                                    ? { ...entry, deleted: !entry.deleted }
                                    : entry,
                                ),
                              }
                            : current,
                        )
                      }
                    />
                  ) : null}
                  {editControls}
                </div>
              </div>
              {view === 'diff' && edit && !selected.binary ? (
                <div className="soha-configuration-data-diff-labels">
                  <span>{zh ? '修改前 · 只读' : 'Before · Read-only'}</span>
                  <span>{zh ? '修改后 · 草稿' : 'After · Draft'}</span>
                </div>
              ) : null}
              <div className="soha-configuration-data-editor">
                {!visible ? (
                  <ManagementState
                    bordered={false}
                    title={zh ? '内容已隐藏' : 'Content hidden'}
                    description={
                      zh
                        ? '点击显示内容查看此 Key 的完整值。'
                        : 'Show content to view the full value of this key.'
                    }
                  />
                ) : (
                  <Suspense
                    fallback={
                      <div className="soha-configuration-data-loading">
                        <Spin />
                      </div>
                    }
                  >
                    <ConfigurationValueEditor
                      key={`${Boolean(edit)}/${selected.id}`}
                      name={selected.key}
                      value={displayedValue}
                      original={edit && !selected.binary ? originalValue(selected) : undefined}
                      comparing={view === 'diff'}
                      readOnly={!editable}
                      onChange={(value) => updateSelected({ value })}
                    />
                  </Suspense>
                )}
              </div>
              <footer className="soha-configuration-data-footer">
                <span>
                  {selected.binary && !secret ? 'binaryData' : 'data'} / {selected.key || '—'}
                </span>
                <span>
                  {edit && entryChanged(selected)
                    ? zh
                      ? '尚未应用到集群'
                      : 'Not applied to cluster'
                    : zh
                      ? '与集群一致'
                      : 'Matches cluster'}
                </span>
              </footer>
            </>
          ) : (
            <>
              <div className="soha-configuration-data-document-header">
                <strong>{zh ? '数据内容' : 'Data content'}</strong>
                {editControls}
              </div>
              <ManagementState
                bordered={false}
                description={
                  zh ? '暂无数据，可通过编辑数据新增 Key。' : 'No data. Edit data to add a key.'
                }
              />
            </>
          )}
        </section>
      </Card>
    </div>
  )
}
