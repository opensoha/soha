import { useEffect, useState } from 'react'
import { Button, Checkbox, Space, Typography } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { memoryApi } from './api'
import { memoryKeys } from './keys'
import type { MemoryPolicy } from './types'

export function MemoryConsent({
  mode,
  policy,
  disabled,
}: {
  mode: 'chat' | 'heya'
  policy?: MemoryPolicy
  disabled: boolean
}) {
  const client = useQueryClient()
  const [enabled, setEnabled] = useState(false)
  const [read, setRead] = useState(false)
  const [write, setWrite] = useState(false)
  const [automatic, setAutomatic] = useState(false)
  useEffect(() => {
    setEnabled(policy?.enabled ?? false)
    setRead(policy?.readEnabled ?? policy?.enabled ?? false)
    setWrite(policy?.writeEnabled ?? policy?.enabled ?? false)
    setAutomatic(policy ? !policy.explicitWriteOnly : false)
  }, [policy])
  const save = useOperationMutation(
    {
      mutationFn: () =>
        memoryApi.policies.create({
          id: policy?.id ?? `personal-${mode}`,
          name: `${mode === 'heya' ? 'Heya' : 'Chat'} 个人记忆`,
          mode,
          consentMode: !enabled
            ? 'disabled'
            : mode === 'heya' && automatic
              ? 'automatic'
              : 'explicit',
          ttlDays: policy ? Math.max(1, Math.round(policy.defaultTtl / 864e11)) : 30,
          readEnabled: read,
          writeEnabled: write,
        }),
      onSuccess: async () => {
        await client.invalidateQueries({ queryKey: memoryKeys.policies() })
        await client.invalidateQueries({ queryKey: memoryKeys.records() })
      },
    },
    { label: ['保存记忆授权', 'Save memory consent'], target: () => mode },
  )
  const blocked = disabled || save.blocked
  return (
    <Space orientation="vertical">
      <Typography.Text strong>{mode === 'heya' ? 'Heya' : 'Chat'} 记忆授权</Typography.Text>
      <Checkbox
        disabled={blocked}
        checked={enabled}
        onChange={(event) => setEnabled(event.target.checked)}
      >
        启用记忆
      </Checkbox>
      <Checkbox
        disabled={blocked || !enabled}
        checked={read}
        onChange={(event) => setRead(event.target.checked)}
      >
        允许读取记忆
      </Checkbox>
      <Checkbox
        disabled={blocked || !enabled}
        checked={write}
        onChange={(event) => setWrite(event.target.checked)}
      >
        允许写入记忆
      </Checkbox>
      {mode === 'heya' ? (
        <Checkbox
          disabled={blocked || !enabled || !write}
          checked={automatic}
          onChange={(event) => setAutomatic(event.target.checked)}
        >
          主动启用 Heya 自动记忆
        </Checkbox>
      ) : null}
      <Typography.Paragraph type="secondary">
        关闭后停止后续使用，已有记忆仍保留；删除条目后立即停用，外部删除传播可能尚未完成。普通对话可以继续。
      </Typography.Paragraph>
      {save.feedback}
      <Button disabled={blocked} loading={save.isPending} onClick={() => save.mutate()}>
        保存记忆授权
      </Button>
    </Space>
  )
}
