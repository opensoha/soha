import { OperationFeedback, useInlineApiError } from '@/components/request-feedback'
import type { PersonalAccessToken } from '@/features/copilot'
import { isApiError } from '@/services/api-error'
import { useAuthStore } from '@/stores/auth-store'
import { useMutation } from '@tanstack/react-query'
import { Alert, Button, Space, Typography } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { authProfileApi } from './profile-api'
import { CredentialRequestNotSentError } from './governed-credentials-api'

interface PendingRotation {
  owner: string
  tokenId: string
  requestId: string
  expiresAt: string
}
const storageKey = (owner: string) => `soha-pat-rotation:${owner}`
function readPending(owner: string): PendingRotation | null {
  if (!owner) return null
  try {
    const value = JSON.parse(
      sessionStorage.getItem(storageKey(owner)) || 'null',
    ) as PendingRotation | null
    return value?.owner === owner &&
      /^[a-f0-9-]{36}$/.test(value.requestId) &&
      value.tokenId &&
      Number.isFinite(Date.parse(value.expiresAt))
      ? value
      : null
  } catch {
    return null
  }
}

export function useProfileKeyRotation(
  owner: string,
  onValue: (value: string, prefix?: string) => void,
  onRefresh: () => void,
) {
  const [record, setRecord] = useState<PendingRotation | null>(null)
  const [recoveryError, setRecoveryError] = useState<Error | null>(null)
  const [receipt, setReceipt] = useState<string | null>(null)
  const [recovering, setRecovering] = useState(false)
  const generation = useRef(0)
  const fence = useRef(false)
  const current = useRef<PendingRotation | null>(null)
  useInlineApiError(recoveryError)
  useEffect(() => {
    generation.current += 1
    current.current = readPending(owner)
    setRecord(current.current)
    setReceipt(null)
    setRecoveryError(null)
    return () => {
      generation.current += 1
    }
  }, [owner])
  function finish(pending: PendingRotation) {
    sessionStorage.removeItem(storageKey(pending.owner))
    current.current = null
    setRecord(null)
    onRefresh()
  }
  const mutation = useMutation({
    gcTime: 0,
    retry: false,
    mutationFn: async (tokenId: string) => {
      const run = generation.current
      if (!owner) throw new Error('请先读取当前账号')
      let pending = current.current
      if (pending && (pending.owner !== owner || pending.tokenId !== tokenId))
        throw new Error('请先核对上一笔轮换')
      const createdThisAttempt = !pending
      if (!pending) {
        pending = {
          owner,
          tokenId,
          requestId: crypto.randomUUID(),
          expiresAt: new Date(Date.now() + 90 * 86400000).toISOString(),
        }
        sessionStorage.setItem(storageKey(owner), JSON.stringify(pending))
        current.current = pending
        setRecord(pending)
      }
      let response: Awaited<ReturnType<typeof authProfileApi.rotateGatewayToken>>
      try {
        response = await authProfileApi.rotateGatewayToken(tokenId, {
          rotationRequestId: pending.requestId,
          expiresAt: pending.expiresAt,
        })
      } catch (cause) {
        // A failed retry cannot prove that the earlier POST was never committed.
        if (createdThisAttempt && cause instanceof CredentialRequestNotSentError) {
          sessionStorage.removeItem(storageKey(pending.owner))
          if (run === generation.current && current.current?.requestId === pending.requestId) {
            current.current = null
            setRecord(null)
          }
        }
        throw cause
      }
      if (run !== generation.current || useAuthStore.getState().user?.userId !== owner) return
      if (response.data.value) onValue(response.data.value, response.data.token?.tokenPrefix)
      finish(pending)
      setReceipt('Key 已轮换，旧 Key 已撤销。新明文仅在本次窗口展示。')
    },
    onSettled: () => {
      fence.current = false
    },
  })
  const submit = (tokenId: string) => {
    if (fence.current || recovering) return
    fence.current = true
    setReceipt(null)
    setRecoveryError(null)
    mutation.mutate(tokenId)
  }
  async function recover() {
    const pending = current.current
    if (!pending || fence.current) return
    const run = generation.current
    fence.current = true
    setRecovering(true)
    setRecoveryError(null)
    try {
      const outcome = await authProfileApi.tokenRotation(pending.requestId)
      if (run !== generation.current || useAuthStore.getState().user?.userId !== owner) return
      if (
        outcome.previousTokenId !== pending.tokenId ||
        outcome.rotationRequestId !== pending.requestId ||
        outcome.tokenKind !== 'personal_access_token' ||
        outcome.status !== 'completed'
      )
        throw new Error('轮换记录与原请求不一致')
      finish(pending)
      mutation.reset()
      setReceipt(
        `轮换已完成，旧 Key 已撤销。替代 Key ID：${outcome.replacementTokenId}。新明文不能恢复；如果未保存，可撤销替代 Key 后重新生成。`,
      )
    } catch (cause) {
      if (run === generation.current)
        setRecoveryError(
          isApiError(cause) && cause.status === 404
            ? new Error('尚未查到原轮换记录。可重试原轮换；服务端会沿用同一请求 ID，避免重复生成。')
            : cause instanceof Error
              ? cause
              : new Error('暂时无法核对原轮换'),
        )
    } finally {
      fence.current = false
      if (run === generation.current) setRecovering(false)
    }
  }
  return {
    error: mutation.error,
    isPending: mutation.isPending,
    blocked: mutation.isPending || recovering || Boolean(record),
    mutate: (token: PersonalAccessToken) => {
      if (!record) submit(token.id)
    },
    feedback: (
      <>
        <OperationFeedback
          label="轮换个人访问凭据"
          pending={mutation.isPending}
          error={mutation.error}
          receipt={receipt ? { message: receipt } : undefined}
        />
        {record ? (
          <Alert
            showIcon
            type="warning"
            title="保留原轮换请求供核对"
            description={
              <Space orientation="vertical">
                <Typography.Text>
                  请求 ID：{record.requestId}。轮换成功会立即撤销旧
                  Key；网页使用当前账号的独立会话核对。新明文无法从恢复记录获取。
                </Typography.Text>
                {recoveryError ? <Typography.Text>{recoveryError.message}</Typography.Text> : null}
                <Space wrap>
                  <Button
                    loading={recovering}
                    disabled={mutation.isPending}
                    onClick={() => void recover()}
                  >
                    核对轮换结果
                  </Button>
                  <Button
                    disabled={mutation.isPending || recovering}
                    onClick={() => submit(record.tokenId)}
                  >
                    重试原轮换
                  </Button>
                </Space>
              </Space>
            }
          />
        ) : null}
      </>
    ),
  }
}
