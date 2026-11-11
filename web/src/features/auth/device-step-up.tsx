import { providerPortalMFA, requestPasskeyAssertion } from '@/features/provider-portal'
import { useInlineApiError } from '@/components/request-feedback'
import type { MFACredential } from '@opensoha/contracts/gen/ts/sohaapi'
import { Alert, Button, Form, Input, Space, Spin, Typography } from 'antd'
import { useEffect, useRef, useState } from 'react'

export function DeviceStepUp({ onVerified }: { onVerified: () => void }) {
  const [credentials, setCredentials] = useState<MFACredential[] | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [pending, setPending] = useState(false)
  const [challenge, setChallenge] = useState<{
    id: string
    expiresAt: string
    recovery: boolean
  } | null>(null)
  const controller = useRef(new AbortController())
  const fence = useRef(false)
  useInlineApiError(error)
  useEffect(() => {
    const current = new AbortController()
    controller.current = current
    void providerPortalMFA.credentials().then(
      (items) => {
        if (!current.signal.aborted) setCredentials(items)
      },
      (cause: unknown) => {
        if (!current.signal.aborted)
          setError(cause instanceof Error ? cause : new Error('无法读取验证方式'))
      },
    )
    return () => current.abort()
  }, [])
  async function run(action: () => Promise<void>) {
    if (fence.current) return
    fence.current = true
    setPending(true)
    setError(null)
    try {
      await action()
    } catch (cause) {
      if (!controller.current.signal.aborted)
        setError(cause instanceof Error ? cause : new Error('验证未完成，请重试'))
    } finally {
      fence.current = false
      if (!controller.current.signal.aborted) setPending(false)
    }
  }
  const beginCode = (recovery: boolean) =>
    run(async () => {
      const result = recovery
        ? await providerPortalMFA.beginRecovery()
        : await providerPortalMFA.beginTOTP()
      if ('provisioningUri' in result && result.provisioningUri)
        throw new Error('请使用已注册的验证方式')
      if (!controller.current.signal.aborted)
        setChallenge({ id: result.challengeId, expiresAt: result.expiresAt, recovery })
    })
  const passkey = () =>
    run(async () => {
      const options = await providerPortalMFA.beginPasskey()
      if (controller.current.signal.aborted) return
      const response = await requestPasskeyAssertion(options, controller.current.signal)
      if (!response) return
      const result = await providerPortalMFA.verifyPasskey(options.challengeId, response)
      if (!result.verified) throw new Error('Passkey 验证未通过')
      if (!controller.current.signal.aborted) onVerified()
    })
  return (
    <section aria-label="安全验证">
      <Typography.Paragraph>
        使用已有验证方式完成安全验证，然后重新核对 CLI 登录请求。
      </Typography.Paragraph>
      {error ? (
        <Alert showIcon type="error" title="验证未完成" description={error.message} />
      ) : null}
      {credentials === null ? (
        <Spin description="正在读取验证方式" />
      ) : (
        <Space wrap>
          {credentials.some((item) => item.type === 'totp') ? (
            <Button disabled={pending} onClick={() => void beginCode(false)}>
              使用认证器
            </Button>
          ) : null}
          {credentials.some((item) => item.type === 'webauthn') ? (
            <Button
              disabled={pending || !window.PublicKeyCredential || !navigator.credentials}
              onClick={() => void passkey()}
            >
              使用 Passkey
            </Button>
          ) : null}
          <Button disabled={pending} onClick={() => void beginCode(true)}>
            使用恢复码
          </Button>
        </Space>
      )}
      {challenge ? (
        <Form
          key={challenge.id}
          layout="vertical"
          onFinish={({ response }: { response: string }) =>
            void run(async () => {
              if (Date.parse(challenge.expiresAt) <= Date.now()) {
                setChallenge(null)
                throw new Error('验证挑战已过期，请重新选择验证方式')
              }
              const result = await providerPortalMFA.verify(challenge.id, response.trim())
              if (!result.verified) throw new Error('验证未通过')
              if (!controller.current.signal.aborted) {
                setChallenge(null)
                onVerified()
              }
            })
          }
        >
          <Form.Item
            label={challenge.recovery ? '恢复码' : '认证器验证码'}
            name="response"
            rules={[{ required: true, message: '请输入验证码' }]}
          >
            <Input autoComplete="off" maxLength={challenge.recovery ? 128 : 6} disabled={pending} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={pending}>
            验证
          </Button>
        </Form>
      ) : null}
    </section>
  )
}
