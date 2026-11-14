import { useInlineApiError } from '@/components/request-feedback'
import { isApiError } from '@/services/api-error'
import { useAuthStore } from '@/stores/auth-store'
import { writeOutcomeUnknown } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import type {
  CLIDeviceConfirmation,
  CLIDeviceDecisionRequest,
} from '@opensoha/contracts/gen/ts/sohaapi'
import {
  CheckOutlined,
  CloseOutlined,
  CodeOutlined,
  ExclamationOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { Alert, Button, Form, Input, Spin } from 'antd'
import type { FormInstance } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { restoreAuthSession } from './auth-api'
import { cliAuthApi } from './cli-auth-api'
import { captureDeviceHandle, clearDeviceHandle } from './device-handle'
import { DeviceStepUp } from './device-step-up'
import './cli-device-page.css'

type Phase = 'code' | 'confirm' | 'mfa' | 'uncertain' | 'approved' | 'denied' | 'closed' | 'expired'

function sessionLifetime(seconds: number) {
  for (const [unit, label] of [
    [86400, '天'],
    [3600, '小时'],
    [60, '分钟'],
    [1, '秒'],
  ] as const) {
    if (seconds >= unit && seconds % unit === 0) return `${seconds / unit} ${label}`
  }
  return '-'
}

export function CLIDevicePage() {
  const navigate = useNavigate()
  const accessToken = useAuthStore((state) => state.accessToken)
  const user = useAuthStore((state) => state.user)
  const [handle, setHandle] = useState(captureDeviceHandle)
  const [auth, setAuth] = useState<'checking' | 'ready' | 'login' | 'unavailable'>(
    accessToken ? 'ready' : 'checking',
  )
  const [phase, setPhase] = useState<Phase>('code')
  const [code, setCode] = useState('')
  const [confirmation, setConfirmation] = useState<CLIDeviceConfirmation | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [pending, setPending] = useState(false)
  const [now, setNow] = useState(Date.now())
  const decision = useRef<CLIDeviceDecisionRequest | null>(null)
  const request = useRef<AbortController | null>(null)
  const fence = useRef(false)
  const active = useRef(true)
  const [form] = Form.useForm<{ code: string }>()
  const mountedForm = useRef<FormInstance<{ code: string }> | null>(null)
  useInlineApiError(error)
  useEffect(() => {
    active.current = true
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => {
      active.current = false
      request.current?.abort()
      window.clearInterval(timer)
    }
  }, [])
  useEffect(() => {
    const replaceRequest = () => {
      if (!window.location.hash) return
      request.current?.abort()
      request.current = null
      fence.current = false
      setPending(false)
      setHandle(captureDeviceHandle())
      setConfirmation(null)
      setCode('')
      decision.current = null
      setPhase('code')
      setError(null)
      if (mountedForm.current) form.resetFields()
    }
    window.addEventListener('hashchange', replaceRequest)
    return () => window.removeEventListener('hashchange', replaceRequest)
  }, [form])
  useEffect(() => {
    let disposed = false
    if (accessToken) {
      setAuth('ready')
      return
    }
    request.current?.abort()
    setConfirmation(null)
    decision.current = null
    setPhase('code')
    setAuth('checking')
    void restoreAuthSession().then((status) => {
      if (!disposed)
        setAuth(
          status === 'authenticated'
            ? 'ready'
            : status === 'unauthenticated'
              ? 'login'
              : 'unavailable',
        )
    })
    return () => {
      disposed = true
    }
  }, [accessToken])
  useEffect(() => {
    request.current?.abort()
    setConfirmation(null)
    setCode('')
    decision.current = null
    setPhase('code')
    setError(null)
    if (mountedForm.current) form.resetFields()
  }, [user?.userId, accessToken, form])
  const expired =
    phase === 'expired' ||
    !handle ||
    now >= handle.expiresAt ||
    (confirmation && now >= Date.parse(confirmation.expiresAt))
  const confirmationExpired = Boolean(
    confirmation && now >= Date.parse(confirmation.confirmationExpiresAt),
  )
  async function execute(action: (signal: AbortSignal) => Promise<void>) {
    if (fence.current) return
    fence.current = true
    const controller = new AbortController()
    request.current = controller
    setPending(true)
    setError(null)
    try {
      await action(controller.signal)
    } catch (cause) {
      if (!controller.signal.aborted && active.current) {
        const failure = cause instanceof Error ? cause : new Error('请求未完成，请重试')
        setError(failure)
        if (isApiError(failure) && failure.code === 'expired_token') {
          setPhase('expired')
          setConfirmation(null)
          decision.current = null
          clearDeviceHandle()
        } else if (isApiError(failure) && failure.code === 'interactive_auth_required')
          setPhase('mfa')
        else if (decision.current && writeOutcomeUnknown(failure)) setPhase('uncertain')
        else if (isApiError(failure) && [409, 410].includes(failure.status)) setPhase('closed')
      }
    } finally {
      if (request.current === controller) {
        fence.current = false
        if (active.current) setPending(false)
      }
    }
  }
  const inspect = (value: string) =>
    execute(async (signal) => {
      if (!handle || expired || !/^[0-9]{6}$/.test(value)) return
      const result = await cliAuthApi.inspect(
        { verificationHandle: handle.value, userCode: value },
        signal,
      )
      if (
        signal.aborted ||
        !active.current ||
        useAuthStore.getState().user?.userId !== user?.userId
      )
        return
      setCode(value)
      setConfirmation(result)
      decision.current = null
      setPhase('confirm')
      form.resetFields()
    })
  const submitDecision = (choice?: 'approve' | 'deny') =>
    execute(async (signal) => {
      if (!handle || !confirmation || expired) return
      if (choice)
        decision.current = {
          verificationHandle: handle.value,
          userCode: code,
          confirmationId: confirmation.confirmationId,
          decision: choice,
        }
      const original = decision.current
      if (!original) return
      const result = await cliAuthApi.decide(original, signal)
      if (
        signal.aborted ||
        !active.current ||
        useAuthStore.getState().user?.userId !== user?.userId
      )
        return
      setPhase(
        result.status === 'approved' || result.status === 'issued'
          ? 'approved'
          : result.status === 'denied'
            ? 'denied'
            : 'closed',
      )
      setConfirmation(null)
      setCode('')
      decision.current = null
      clearDeviceHandle()
    })
  const terminal = ['approved', 'denied', 'closed'].includes(phase)
  const title = !handle
    ? '请打开终端中的完整登录地址'
    : terminal
      ? phase === 'approved'
        ? '已批准登录'
        : phase === 'denied'
          ? '已拒绝登录'
          : '请求已处理或不可用'
      : expired
        ? '登录请求已过期'
        : phase === 'mfa'
          ? '完成安全验证'
          : confirmation
            ? '允许此终端登录？'
            : '连接到 Soha CLI'
  const description = !handle
    ? '返回终端重新运行 soha login，然后打开本次提供的地址。'
    : terminal
      ? phase === 'approved'
        ? '回到终端继续完成登录。你现在可以关闭此页面。'
        : '回到终端查看结果。需要登录时，请重新发起请求。'
      : expired
        ? '请回到终端重新发起登录，再打开新的登录地址。'
        : phase === 'mfa'
          ? '验证当前账号后，继续核对你的登录请求。'
          : confirmation
            ? '请确认这是你刚刚发起登录的终端。'
            : '输入终端中显示的六位验证码以继续。'
  const account =
    confirmation?.user.userName ||
    user?.displayName ||
    user?.userName ||
    user?.username ||
    '已登录用户'
  const instance =
    window.location.origin + window.location.pathname.replace(/\/auth\/device\/?$/, '')
  const stopped = !handle || terminal || expired
  const remainingSeconds = Math.max(
    0,
    Math.ceil(
      (Math.min(
        handle?.expiresAt ?? now,
        confirmation ? Date.parse(confirmation.expiresAt) : Infinity,
      ) -
        now) /
        1000,
    ),
  )
  const remainingLabel = '授权剩余时间'
  const remainingAnnouncement =
    remainingSeconds > 60
      ? `${remainingLabel}，不到 ${Math.ceil(remainingSeconds / 60)} 分钟`
      : `${remainingLabel}，不到 1 分钟`
  return (
    <main className="soha-cli-device-page">
      <section className="soha-cli-device-content" aria-labelledby="device-title">
        <div className="soha-cli-device-brand">
          <img src={`${import.meta.env.BASE_URL}logo.svg`} alt="" />
          <span>Soha</span>
        </div>
        <header className="soha-cli-device-heading" aria-live="polite">
          {stopped ? (
            <div
              className={`soha-cli-device-result ${phase === 'approved' ? 'is-approved' : ''}`}
              aria-hidden="true"
            >
              {phase === 'approved' ? (
                <CheckOutlined />
              ) : phase === 'denied' ? (
                <CloseOutlined />
              ) : (
                <ExclamationOutlined />
              )}
            </div>
          ) : null}
          <h1 id="device-title">{title}</h1>
          <p>{description}</p>
        </header>
        {!stopped && confirmation ? (
          <div className="soha-cli-device-countdown">
            <p className="soha-cli-device-note" role="timer" aria-live="off">
              {remainingLabel}：{Math.floor(remainingSeconds / 60)} 分{' '}
              {String(remainingSeconds % 60).padStart(2, '0')} 秒
            </p>
            <span
              className="soha-cli-device-sr-only"
              role="status"
              aria-live="polite"
              aria-atomic="true"
            >
              {remainingAnnouncement}
            </span>
          </div>
        ) : !stopped ? (
          <p className="soha-cli-device-note">登录请求实际有效期以终端为准。</p>
        ) : null}
        {stopped ? (
          phase === 'approved' ? (
            <p className="soha-cli-device-note">需要撤销登录？前往个人中心的 CLI 会话。</p>
          ) : null
        ) : auth === 'checking' ? (
          <div className="soha-cli-device-loading" role="status">
            <Spin />
            <span>正在检查网页登录状态</span>
          </div>
        ) : auth === 'login' ? (
          <div className="soha-cli-device-flow">
            <p className="soha-cli-device-note">
              先登录当前 Soha 实例，也可以使用已配置的第三方账号。
            </p>
            <Button
              type="primary"
              size="large"
              block
              onClick={() => navigate('/login?return_to=%2Fauth%2Fdevice')}
            >
              登录并继续
            </Button>
          </div>
        ) : auth === 'unavailable' ? (
          <Alert
            showIcon
            type="warning"
            title="暂时无法检查登录状态"
            action={<Button onClick={() => window.location.reload()}>重新检查</Button>}
          />
        ) : (
          <>
            <div className="soha-cli-device-account">
              <UserOutlined aria-hidden="true" />
              <span>
                <span className="soha-cli-device-account-label">授权账号</span>
                <strong>{account}</strong>
              </span>
            </div>
            {error ? (
              <Alert
                showIcon
                type={phase === 'uncertain' ? 'warning' : 'error'}
                title={phase === 'uncertain' ? '原决定的结果尚未确认' : '请求未完成'}
                description={
                  phase === 'uncertain'
                    ? '保留了原决定。核对时将再次提交同一决定，服务端只处理一次。'
                    : error.message
                }
              />
            ) : null}
            {phase === 'mfa' ? (
              <DeviceStepUp
                onVerified={() => {
                  setPhase('code')
                  setError(null)
                  setConfirmation(null)
                  decision.current = null
                }}
              />
            ) : confirmation ? (
              <>
                <div className="soha-cli-device-request">
                  <CodeOutlined className="soha-cli-device-terminal" aria-hidden="true" />
                  <div>
                    <span className="soha-cli-device-account-label">请求登录的终端</span>
                    <strong>{confirmation.deviceName}</strong>
                    <span className="soha-cli-device-instance">{instance}</span>
                  </div>
                </div>
                <p className="soha-cli-device-lifetime">
                  会话最长有效期{' '}
                  <strong>{sessionLifetime(confirmation.sessionAbsoluteExpiresIn)}</strong>
                </p>
                <div className="soha-cli-device-disclosures">
                  <details className="soha-cli-device-details">
                    <summary>允许的权限（{confirmation.permissionKeys.length}）</summary>
                    <ul tabIndex={0} aria-label="完整授权权限">
                      {confirmation.permissionKeys.map((key) => (
                        <li key={key}>
                          <code>{key}</code>
                        </li>
                      ))}
                    </ul>
                  </details>
                  <details className="soha-cli-device-details">
                    <summary>请求详情</summary>
                    <dl>
                      <dt>CLI 版本</dt>
                      <dd>{confirmation.clientVersion}</dd>
                      <dt>请求有效至</dt>
                      <dd>{formatDateTime(confirmation.expiresAt)}</dd>
                    </dl>
                  </details>
                </div>
                {phase === 'uncertain' ? (
                  <Button
                    type="primary"
                    size="large"
                    block
                    loading={pending}
                    onClick={() => void submitDecision()}
                  >
                    核对原决定
                  </Button>
                ) : confirmationExpired ? (
                  <Alert
                    showIcon
                    type="warning"
                    title="确认已过期"
                    action={
                      <Button
                        disabled={pending}
                        onClick={() => {
                          setConfirmation(null)
                          setPhase('code')
                          setError(null)
                        }}
                      >
                        重新输入验证码
                      </Button>
                    }
                  />
                ) : (
                  <div className="soha-cli-device-actions">
                    <Button
                      type="primary"
                      size="large"
                      block
                      loading={pending}
                      onClick={() => void submitDecision('approve')}
                    >
                      批准此终端登录
                    </Button>
                    <Button
                      type="text"
                      size="large"
                      block
                      disabled={pending}
                      onClick={() => void submitDecision('deny')}
                    >
                      拒绝
                    </Button>
                  </div>
                )}
                <p className="soha-cli-device-note">
                  终端名称由请求方提供。仅批准你本人发起的请求。
                </p>
              </>
            ) : (
              <Form
                form={form}
                ref={mountedForm}
                preserve={false}
                layout="vertical"
                className="soha-cli-device-form"
                onFinish={({ code: value }) => void inspect(value)}
              >
                <Form.Item
                  name="code"
                  label="终端显示的六位验证码"
                  normalize={(value: string) => value.replace(/[ \t\r\n]/g, '')}
                  rules={[
                    {
                      pattern: /^[0-9]{6}$/,
                      required: true,
                      message: '请输入六位数字，保留开头的 0',
                    },
                  ]}
                >
                  <Input
                    className="soha-cli-device-code"
                    size="large"
                    inputMode="numeric"
                    autoComplete="off"
                    maxLength={6}
                    onPaste={(event) => {
                      const pasted = event.clipboardData.getData('text').replace(/[ \t\r\n]/g, '')
                      if (/^[0-9]{6}$/.test(pasted)) {
                        event.preventDefault()
                        form.setFieldValue('code', pasted)
                      }
                    }}
                    placeholder="000000"
                    disabled={pending}
                  />
                </Form.Item>
                <Button type="primary" size="large" block htmlType="submit" loading={pending}>
                  核对登录请求
                </Button>
                <p className="soha-cli-device-note">验证码仅用于核对请求，下一步将由你确认授权。</p>
              </Form>
            )}
          </>
        )}
        <footer className="soha-cli-device-footer">Soha CLI · 设备授权</footer>
      </section>
    </main>
  )
}
