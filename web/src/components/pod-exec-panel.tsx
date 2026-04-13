import { useEffect, useState } from 'react'
import { Button, Card, Descriptions, Empty, InputNumber, Select, Tag, TextArea, Toast, Typography } from '@douyinfe/semi-ui'
import { useMutation } from '@tanstack/react-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { formatDateTime } from '@/utils/time'
import type { ApiResponse, PodExecResult } from '@/types'

const { Text } = Typography

const COMMAND_PRESETS = [
  'printenv | head -n 20',
  'ps aux | head -n 20',
  'ls -lah /',
]

export function PodExecPanel({
  clusterId,
  namespace,
  podName,
  containerOptions,
}: {
  clusterId?: string | null
  namespace?: string | null
  podName: string
  containerOptions: Array<{ value: string; label: string }>
}) {
  const { localeCode } = useI18n()
  const [command, setCommand] = useState(COMMAND_PRESETS[0])
  const [timeoutSeconds, setTimeoutSeconds] = useState(10)
  const [container, setContainer] = useState('')
  const [result, setResult] = useState<PodExecResult | null>(null)

  useEffect(() => {
    if (container) return
    if (containerOptions.length > 0) {
      setContainer(String(containerOptions[0].value))
    }
  }, [container, containerOptions])

  const execMutation = useMutation({
    mutationFn: async () => {
      if (!clusterId || !namespace) {
        throw new Error(localeCode === 'zh_CN' ? '缺少集群或命名空间上下文' : 'Cluster or namespace context is missing')
      }
      return api.post<ApiResponse<PodExecResult>>(
        `/clusters/${clusterId}/workloads/pods/${encodeURIComponent(podName)}/exec?namespace=${encodeURIComponent(namespace)}`,
        {
          command,
          container: container || undefined,
          timeoutSeconds,
        },
      )
    },
    onSuccess: (response) => {
      setResult(response.data)
      Toast.success(localeCode === 'zh_CN' ? '命令执行完成' : 'Command executed')
    },
    onError: (err: Error) => Toast.error(err.message),
  })

  if (!clusterId || !namespace) {
    return <Empty description={localeCode === 'zh_CN' ? '请选择有效的集群和命名空间后再执行命令' : 'Select a valid cluster and namespace before running commands'} />
  }

  return (
    <div className="kc-page-section">
      <Card className="kc-detail-card" title={localeCode === 'zh_CN' ? '命令执行' : 'Command Exec'}>
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2">
            <Text strong>{localeCode === 'zh_CN' ? '快捷命令' : 'Presets'}</Text>
            {COMMAND_PRESETS.map((item) => (
              <Button key={item} size="small" theme="light" onClick={() => setCommand(item)}>
                {item}
              </Button>
            ))}
          </div>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-[220px_minmax(0,1fr)_160px]">
            <div className="flex flex-col gap-2">
              <Text strong>{localeCode === 'zh_CN' ? '容器' : 'Container'}</Text>
              <Select
                value={container || undefined}
                onChange={(value) => setContainer(String(value || ''))}
                optionList={containerOptions}
                placeholder={localeCode === 'zh_CN' ? '选择容器' : 'Select container'}
                showClear
              />
            </div>
            <div className="flex flex-col gap-2">
              <Text strong>{localeCode === 'zh_CN' ? '命令' : 'Command'}</Text>
              <TextArea
                value={command}
                onChange={setCommand}
                autosize={{ minRows: 3, maxRows: 6 }}
                placeholder={localeCode === 'zh_CN' ? '请输入要在容器中执行的命令' : 'Enter the command to run inside the container'}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Text strong>{localeCode === 'zh_CN' ? '超时(秒)' : 'Timeout (s)'}</Text>
              <InputNumber value={timeoutSeconds} min={1} max={120} onChange={(value) => setTimeoutSeconds(Number(value) || 10)} />
            </div>
          </div>

          <div className="flex justify-end">
            <Button
              theme="solid"
              type="primary"
              loading={execMutation.isPending}
              disabled={!command.trim()}
              onClick={() => execMutation.mutate()}
            >
              {localeCode === 'zh_CN' ? '执行命令' : 'Run Command'}
            </Button>
          </div>
        </div>
      </Card>

      {result ? (
        <>
          <Card
            className="kc-detail-card"
            title={localeCode === 'zh_CN' ? '执行结果' : 'Execution Result'}
            headerExtraContent={<Tag color={result.success ? 'green' : 'red'}>{result.success ? (localeCode === 'zh_CN' ? '成功' : 'Success') : (localeCode === 'zh_CN' ? '失败' : 'Failed')}</Tag>}
          >
            <Descriptions
              data={[
                { key: localeCode === 'zh_CN' ? '命令' : 'Command', value: result.command },
                { key: localeCode === 'zh_CN' ? '容器' : 'Container', value: result.container || '-' },
                { key: localeCode === 'zh_CN' ? '执行时间' : 'Executed At', value: formatDateTime(result.executedAt) },
                { key: 'Stdout', value: `${result.stdoutBytes} B${result.stdoutTruncated ? ` (${localeCode === 'zh_CN' ? '已截断' : 'truncated'})` : ''}` },
                { key: 'Stderr', value: `${result.stderrBytes} B${result.stderrTruncated ? ` (${localeCode === 'zh_CN' ? '已截断' : 'truncated'})` : ''}` },
                { key: localeCode === 'zh_CN' ? '退出信息' : 'Exit Message', value: result.exitMessage || '-' },
              ]}
            />
          </Card>

          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <Card className="kc-detail-card" title="Stdout">
              <pre className="kc-code-block kc-code-block-dark">{result.stdout || (localeCode === 'zh_CN' ? '无标准输出' : 'No stdout output')}</pre>
            </Card>
            <Card className="kc-detail-card" title="Stderr">
              <pre className="kc-code-block">{result.stderr || (localeCode === 'zh_CN' ? '无标准错误输出' : 'No stderr output')}</pre>
            </Card>
          </div>
        </>
      ) : (
        <Card className="kc-detail-card">
          <Empty description={localeCode === 'zh_CN' ? '执行命令后会在这里展示 stdout/stderr 输出' : 'Stdout and stderr output will appear here after execution'} />
        </Card>
      )}
    </div>
  )
}
