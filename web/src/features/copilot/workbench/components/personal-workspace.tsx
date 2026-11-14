import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import type {
  PersonalEnvironment,
  PersonalGuestRuntime,
  PersonalGuestRuntimeInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Drawer, Input, Space, Tabs, Typography } from 'antd'
import { useCallback, useEffect, useRef, useState } from 'react'
import { workbenchKeys } from '../keys'
import { workbenchMutations } from '../mutations'
import { workbenchQueries } from '../queries'

const states: Record<string, string> = {
  queued: '等待执行',
  running: '执行中',
  succeeded: '已完成',
  failed: '执行失败',
  canceled: '已取消',
  unknown: '结果未确认',
}
function encodeText(value: string) {
  return btoa(String.fromCharCode(...new TextEncoder().encode(value)))
}
function decodeText(value: string) {
  return new TextDecoder('utf-8', { fatal: true }).decode(
    Uint8Array.from(atob(value), (v) => v.charCodeAt(0)),
  )
}

export function PersonalWorkspace({
  owner,
  environment,
  onRefresh,
}: {
  owner: string
  environment: PersonalEnvironment
  onRefresh: () => void
}) {
  const { message } = App.useApp()
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const [tab, setTab] = useState('command')
  const [argv, setArgv] = useState('["/usr/bin/printf","Hello Soha\\n"]')
  const [path, setPath] = useState('notes/example.txt')
  const [content, setContent] = useState('')
  const [fileHash, setFileHash] = useState('')
  const [url, setURL] = useState('https://example.com/')
  const [selector, setSelector] = useState('')
  const [text, setText] = useState('')
  const [toolsID, setToolsID] = useState('')
  const [frame, setFrame] = useState<PersonalGuestRuntime['browser']>()
  const loadedFile = useRef('')
  const query = useQuery(workbenchQueries.workspaceRuntime(owner, environment.assistantId, open))
  const toolsQuery = useQuery(
    workbenchQueries.guestToolsOperation(owner, environment.assistantId, toolsID),
  )
  const refresh = () => {
    onRefresh()
    void client.invalidateQueries({
      queryKey: workbenchKeys.workspaceRuntime(owner, environment.assistantId),
    })
  }
  const operation = useMutation({
    ...workbenchMutations.runtime(environment.assistantId),
    onSettled: refresh,
    onError: () => void message.error('操作结果未确认，请读取原操作状态。不会自动重复执行。'),
  })
  const tools = useMutation({
    ...workbenchMutations.guestTools(),
    onSuccess: (r) => setToolsID(r.data!.id),
    onSettled: onRefresh,
    onError: () => void message.error('工具接通请求未确认，请核对环境。'),
  })
  const denied =
    queryAccessDenied(query) ||
    queryAccessDenied(toolsQuery) ||
    queryAccessDenied(operation) ||
    queryAccessDenied(tools)
  const current = denied ? undefined : readableQueryData(query)?.data
  const clearDraft = useCallback(() => {
    setText('')
    setContent('')
    setFileHash('')
    setFrame(undefined)
    setArgv('["/usr/bin/printf","Hello Soha\\n"]')
    setPath('notes/example.txt')
    setURL('https://example.com/')
    setSelector('')
  }, [])
  useEffect(() => {
    if (denied) {
      setOpen(false)
      clearDraft()
    }
  }, [denied, clearDraft])
  const latest = current?.latest
  const control = current?.browserControl
  const busy =
    operation.isPending || tools.isPending || ['queued', 'running'].includes(latest?.status ?? '')
  const ready =
    !query.isError &&
    environment.state === 'ready' &&
    !!environment.expiresAt &&
    Date.parse(environment.expiresAt) > Date.now()
  const controlled =
    ready && control?.owner === 'user' && Date.parse(control.expiresAt) > Date.now()
  const canPoint =
    controlled &&
    !!frame &&
    frame.controlId === control.controlId &&
    frame.generation === control.generation &&
    frame.instanceId === control.instanceId
  useEffect(() => {
    if (latest?.browser) setFrame(latest.browser)
  }, [latest])
  useEffect(() => {
    if (
      latest?.file?.path === path &&
      latest.status === 'succeeded' &&
      loadedFile.current !== latest.id
    ) {
      loadedFile.current = latest.id
      setFileHash(latest.file.sha256)
      if (latest.kind === 'file_read') setContent(decodeText(latest.file.contentBase64 ?? ''))
    }
  }, [latest, path])
  const submit = (
    input: Omit<PersonalGuestRuntimeInput, 'environmentVersion' | 'idempotencyKey'>,
  ) =>
    operation.mutate({
      ...input,
      environmentVersion: environment.version,
      idempotencyKey: crypto.randomUUID(),
    })
  const browser = (
    action: NonNullable<PersonalGuestRuntimeInput['browser']>['action'],
    extra: Partial<NonNullable<PersonalGuestRuntimeInput['browser']>> = {},
  ) =>
    submit({
      kind: 'browser',
      browser: {
        action,
        ...(control ? { controlId: control.controlId, generation: control.generation } : {}),
        ...extra,
      },
    })
  const command = (kind: 'execute' | 'docker') => {
    try {
      const parsed: unknown = JSON.parse(argv)
      if (!Array.isArray(parsed) || parsed.some((v) => typeof v !== 'string')) throw new Error()
      submit({ kind, argv: parsed as string[] })
    } catch {
      void message.error('请输入由字符串组成的命令参数数组。')
    }
  }
  const commands = (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Typography.Text>命令参数</Typography.Text>
      <Input.TextArea
        aria-label="命令参数数组"
        value={argv}
        rows={3}
        maxLength={8192}
        onChange={(e) => setArgv(e.target.value)}
      />
      <Button
        disabled={!ready || busy}
        onClick={() => command(tab === 'docker' ? 'docker' : 'execute')}
      >
        执行{tab === 'docker' ? ' Docker 命令' : '命令'}
      </Button>
    </Space>
  )
  return (
    <>
      <ReadFeedback query={query} label="个人工作空间" />
      <Button disabled={denied} onClick={() => setOpen(true)}>
        打开个人工作空间
      </Button>
      <Drawer
        title="个人工作空间"
        open={open && !denied}
        onClose={() => {
          setOpen(false)
          clearDraft()
        }}
        size="large"
        destroyOnHidden
      >
        {!denied && (
          <Space orientation="vertical" style={{ width: '100%' }}>
            <Typography.Text>
              同一工作空间的状态、文件和浏览器控权由 Web 与 App 共享。
            </Typography.Text>
            <Space wrap>
              <Button onClick={refresh} loading={query.isFetching}>
                读取原操作状态
              </Button>
              <Button
                disabled={query.isError || busy || !!toolsID || environment.state === 'ready'}
                loading={tools.isPending}
                onClick={() =>
                  tools.mutate({
                    environmentVersion: environment.version,
                    idempotencyKey: crypto.randomUUID(),
                  })
                }
              >
                接通执行能力
              </Button>
            </Space>
            {toolsQuery.data?.data && (
              <Typography.Text role="status">
                工具接通：{toolsQuery.data.data.status}；完成后在助手资料中核对环境。
              </Typography.Text>
            )}
            {query.isError && (
              <Alert
                type="error"
                title="工作空间读取失败，操作已暂停"
                action={<Button onClick={() => void query.refetch()}>重试读取</Button>}
              />
            )}
            {!ready && (
              <Alert
                type="info"
                title="环境尚未通过当前就绪检查"
                description="请先接通执行能力并核对环境。聊天仍可继续。"
              />
            )}
            <Tabs
              activeKey={tab}
              onChange={setTab}
              items={[
                { key: 'command', label: '命令', children: commands },
                {
                  key: 'docker',
                  label: 'Docker',
                  children: (
                    <>
                      <Typography.Paragraph>
                        在固定镜像内运行，写入同一持久工作区并受硬配额约束。
                      </Typography.Paragraph>
                      {commands}
                    </>
                  ),
                },
                {
                  key: 'file',
                  label: '文件',
                  children: (
                    <Space orientation="vertical" style={{ width: '100%' }}>
                      <Input
                        aria-label="工作区文件路径"
                        value={path}
                        maxLength={1024}
                        onChange={(e) => {
                          setPath(e.target.value)
                          setFileHash('')
                        }}
                      />
                      <Input.TextArea
                        aria-label="工作区文件内容"
                        value={content}
                        rows={8}
                        onChange={(e) => setContent(e.target.value)}
                      />
                      <Space wrap>
                        <Button
                          disabled={!ready || busy}
                          onClick={() => submit({ kind: 'file_read', file: { path } })}
                        >
                          读取文件
                        </Button>
                        <Button
                          disabled={!ready || busy}
                          onClick={() => {
                            if (new TextEncoder().encode(content).length > 45056) {
                              void message.error('文件最多 44 KiB')
                              return
                            }
                            submit({
                              kind: 'file_write',
                              file: {
                                path,
                                contentBase64: encodeText(content),
                                ...(fileHash ? { expectedSHA256: fileHash } : {}),
                              },
                            })
                          }}
                        >
                          {fileHash ? '保存已核对的文件' : '创建新文件'}
                        </Button>
                      </Space>
                      <Typography.Text type="secondary">
                        更新已有文件前请先读取；内容变化时服务端拒绝覆盖。
                      </Typography.Text>
                    </Space>
                  ),
                },
                {
                  key: 'browser',
                  label: '浏览器',
                  children: (
                    <Space orientation="vertical" style={{ width: '100%' }}>
                      <Typography.Text role="status">
                        浏览器控权：
                        {control?.owner === 'user'
                          ? '用户'
                          : control?.owner === 'agent'
                            ? 'Agent'
                            : '未持有'}
                        {control ? ` · 代次 ${control.generation}` : ''}
                      </Typography.Text>
                      <Space wrap>
                        <Button
                          disabled={!ready || busy}
                          onClick={() =>
                            browser(control?.owner === 'agent' ? 'takeover' : 'acquire')
                          }
                        >
                          接管浏览器
                        </Button>
                        <Button disabled={!controlled || busy} onClick={() => browser('snapshot')}>
                          刷新画面
                        </Button>
                        <Button disabled={!controlled || busy} onClick={() => browser('release')}>
                          释放控权
                        </Button>
                      </Space>
                      <Input
                        aria-label="浏览器地址"
                        value={url}
                        maxLength={4096}
                        onChange={(e) => setURL(e.target.value)}
                      />
                      <Button
                        disabled={!controlled || busy}
                        onClick={() => browser('navigate', { url })}
                      >
                        打开页面
                      </Button>
                      <Input
                        aria-label="浏览器元素选择器"
                        value={selector}
                        maxLength={2048}
                        onChange={(e) => setSelector(e.target.value)}
                      />
                      <Input.Password
                        aria-label="浏览器输入内容"
                        value={text}
                        maxLength={4096}
                        onChange={(e) => setText(e.target.value)}
                        autoComplete="off"
                      />
                      <Space wrap>
                        <Button
                          disabled={!controlled || busy || !selector}
                          onClick={() => browser('click', { selector })}
                        >
                          点击元素
                        </Button>
                        <Button
                          disabled={!controlled || busy || !selector}
                          onClick={() => {
                            browser('type', { selector, text })
                            setText('')
                          }}
                        >
                          输入到元素
                        </Button>
                      </Space>
                      {frame?.jpegBase64 && (
                        <>
                          <Typography.Text>
                            {frame.title || frame.url || '浏览器画面'} · 800 × 600
                          </Typography.Text>
                          <button
                            type="button"
                            disabled={!canPoint || busy}
                            aria-label="在浏览器画面中点击"
                            style={{
                              padding: 0,
                              border: 0,
                              background: 'transparent',
                              width: '100%',
                            }}
                            onClick={(e) => {
                              if (!e.detail) {
                                void message.info('键盘操作请使用元素选择器和点击按钮。')
                                return
                              }
                              const r = e.currentTarget.getBoundingClientRect()
                              browser('pointer', {
                                x: Math.min(
                                  799,
                                  Math.floor(((e.clientX - r.left) * 800) / r.width),
                                ),
                                y: Math.min(
                                  599,
                                  Math.floor(((e.clientY - r.top) * 600) / r.height),
                                ),
                              })
                            }}
                          >
                            <img
                              alt="个人浏览器实际画面"
                              src={`data:image/jpeg;base64,${frame.jpegBase64}`}
                              style={{ display: 'block', width: '100%' }}
                            />
                          </button>
                          {!canPoint && (
                            <Typography.Text type="secondary">
                              保留的观察画面；刷新当前控权后才能操作。
                            </Typography.Text>
                          )}
                        </>
                      )}
                    </Space>
                  ),
                },
              ]}
            />
            {latest && (
              <div role="status">
                <StatusTag value={latest.status} label={states[latest.status] || latest.status} />
                <Typography.Paragraph copyable>{latest.id}</Typography.Paragraph>
                {latest.exitCode !== undefined && <p>退出码：{latest.exitCode}</p>}
                {latest.output && (
                  <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                    {latest.output}
                  </pre>
                )}
              </div>
            )}
          </Space>
        )}
      </Drawer>
    </>
  )
}
