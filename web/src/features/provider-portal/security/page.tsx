import { ContextFeedback } from '@/components/context-feedback'
import { operationModalStyles } from '@/components/modal-styles'
import { OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { identityRuntimeQueries } from '@/features/identity'
import { useI18n } from '@/i18n'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  ArrowLeftOutlined,
  ClockCircleOutlined,
  InfoCircleOutlined,
  KeyOutlined,
  LockOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import type {
  MFAEnrollmentChallenge,
  MFARecoveryChallenge,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  Empty,
  Input,
  Modal,
  Popconfirm,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { providerPortalApi } from '../api'
import { providerPortalMutations } from '../mutations'
import '../provider-portal-pages.css'
import { providerPortalQueries } from '../queries'
import { PortalUserAvatar } from '../shared/account-menu'
import { PortalTagsOrEmpty } from '../shared/application-ui'
import { formatPortalDateTime } from '../shared/formatters'
import { decodeBase64Url, encodeBase64Url, requestPasskeyAssertion } from '../shared/webauthn'

const { Text, Title } = Typography

export function PortalSecurityPage() {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const securityQuery = useQuery(providerPortalQueries.security())
  const runtimeQuery = useQuery(identityRuntimeQueries.capabilities())
  const runtime = readableQueryData(runtimeQuery)
  const mfaAvailable = Boolean(
    runtime?.totp?.available || runtime?.webauthn?.available || runtime?.recoveryCodes?.available,
  )
  const credentialsQuery = useQuery(providerPortalQueries.mfaCredentials(mfaAvailable))
  const readDenied =
    queryAccessDenied(securityQuery) ||
    queryAccessDenied(runtimeQuery) ||
    queryAccessDenied(credentialsQuery)
  const sensitiveGeneration = useRef(0)
  const [totpChallenge, setTOTPChallenge] = useState<MFAEnrollmentChallenge | null>(null)
  const [totpCode, setTOTPCode] = useState('')
  const [recoveryChallenge, setRecoveryChallenge] = useState<MFARecoveryChallenge | null>(null)
  const [recoveryCode, setRecoveryCode] = useState('')
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [nativeError, setNativeError] = useState<Error | null>(null)
  const [nativePending, setNativePending] = useState(false)
  const nativeFence = useRef(false)

  const revokeMutation = useOperationMutation(
    providerPortalMutations.revokeMFACredential(queryClient),
    {
      label: ['撤销 MFA 凭据', 'Revoke MFA credential'],
      target: (id) => id,
    },
  )
  const beginTOTPMutation = useOperationMutation(
    {
      mutationKey: providerPortalMutations.beginTOTPEnrollment().mutationKey,
      gcTime: 0,
      mutationFn: async () => {
        const generation = sensitiveGeneration.current
        const challenge = await providerPortalApi.beginTOTPEnrollment()
        if (generation === sensitiveGeneration.current) setTOTPChallenge(challenge)
        return challenge.challengeId
      },
    },
    {
      label: ['创建 TOTP 注册挑战', 'Begin TOTP enrollment'],
      target: () => '当前账户',
      receipt: () => ({ message: '注册挑战已创建', evidence: '验证完成后才会注册认证器。' }),
    },
  )
  const verifyMutation = useOperationMutation(
    { ...providerPortalMutations.verifyMFAChallenge(queryClient), gcTime: 0 },
    {
      label: ['验证 MFA 挑战', 'Verify MFA challenge'],
      target: (v) => v.challengeId,
      receipt: () => ({ message: 'MFA 挑战已通过', evidence: '请核对当前凭据记录。' }),
    },
  )
  const beginRecoveryMutation = useOperationMutation(
    {
      mutationKey: providerPortalMutations.beginRecoveryChallenge().mutationKey,
      gcTime: 0,
      mutationFn: async () => {
        const generation = sensitiveGeneration.current
        const challenge = await providerPortalApi.beginRecoveryChallenge()
        if (generation === sensitiveGeneration.current) setRecoveryChallenge(challenge)
        return challenge.challengeId
      },
    },
    { label: ['创建恢复码挑战', 'Begin recovery challenge'], target: () => '当前账户' },
  )
  const beginWebAuthnMutation = useOperationMutation(
    { ...providerPortalMutations.beginWebAuthnEnrollment(), gcTime: 0 },
    {
      label: ['创建 Passkey 注册挑战', 'Begin passkey enrollment'],
      target: () => '当前账户',
    },
  )
  const authenticateWebAuthnMutation = useOperationMutation(
    { ...providerPortalMutations.beginWebAuthnAuthentication(), gcTime: 0 },
    { label: ['创建 Passkey 验证挑战', 'Begin passkey verification'], target: () => '当前账户' },
  )
  const verifyWebAuthnMutation = useOperationMutation(
    { ...providerPortalMutations.verifyWebAuthnChallenge(queryClient), gcTime: 0 },
    { label: ['验证 Passkey 挑战', 'Verify passkey challenge'], target: (v) => v.challengeId },
  )
  const recoveryMutation = useOperationMutation(
    {
      mutationKey: providerPortalMutations.regenerateRecoveryCodes().mutationKey,
      gcTime: 0,
      mutationFn: async () => {
        const generation = sensitiveGeneration.current
        const result = await providerPortalApi.regenerateRecoveryCodes()
        if (generation === sensitiveGeneration.current) setRecoveryCodes(result.codes)
        return result.codes.length
      },
    },
    {
      label: ['重新生成恢复码', 'Regenerate recovery codes'],
      target: () => '当前账户',
      receipt: (count) => ({
        message: `已生成 ${count} 个恢复码`,
        evidence: '敏感值仅在专用窗口本次展示。',
      }),
    },
  )

  const credentials =
    credentialsQuery.isEnabled === false ? undefined : readableQueryData(credentialsQuery)
  const denied =
    readDenied ||
    [
      revokeMutation,
      beginTOTPMutation,
      verifyMutation,
      beginRecoveryMutation,
      beginWebAuthnMutation,
      authenticateWebAuthnMutation,
      verifyWebAuthnMutation,
      recoveryMutation,
    ].some(queryAccessDenied)
  const blocked =
    nativePending ||
    denied ||
    [
      revokeMutation,
      beginTOTPMutation,
      verifyMutation,
      beginRecoveryMutation,
      beginWebAuthnMutation,
      authenticateWebAuthnMutation,
      verifyWebAuthnMutation,
      recoveryMutation,
    ].some((m) => m.blocked)

  useEffect(() => {
    if (!denied) return
    sensitiveGeneration.current += 1
    setTOTPChallenge(null)
    setTOTPCode('')
    setRecoveryChallenge(null)
    setRecoveryCode('')
    setRecoveryCodes([])
    setNativeError(null)
  }, [denied])

  useEffect(
    () => () => {
      sensitiveGeneration.current += 1
    },
    [],
  )

  const enrollWebAuthn = async () => {
    if (blocked || nativeFence.current || !window.PublicKeyCredential || !navigator.credentials)
      return
    const generation = sensitiveGeneration.current
    nativeFence.current = true
    setNativePending(true)
    setNativeError(null)
    try {
      const options = await beginWebAuthnMutation.mutateAsync()
      if (generation !== sensitiveGeneration.current) return
      const credential = (await navigator.credentials.create({
        publicKey: {
          challenge: decodeBase64Url(options.challenge),
          rp: { id: options.rpId, name: options.rpName },
          user: {
            id: decodeBase64Url(options.userId),
            name: options.userName,
            displayName: options.userName,
          },
          pubKeyCredParams: options.algorithms.map((alg) => ({ alg, type: 'public-key' })),
          timeout: options.timeoutMilliseconds,
          excludeCredentials: options.excludeCredentialIds?.map((id) => ({
            id: decodeBase64Url(id),
            type: 'public-key',
          })),
        },
      })) as PublicKeyCredential | null
      if (!credential || generation !== sensitiveGeneration.current) return
      const response = credential.response as AuthenticatorAttestationResponse
      await verifyWebAuthnMutation.mutateAsync({
        challengeId: options.challengeId,
        response: {
          credentialId: credential.id,
          clientDataJSON: encodeBase64Url(response.clientDataJSON),
          attestationObject: encodeBase64Url(response.attestationObject),
        },
      })
      void credentialsQuery.refetch()
    } catch (error) {
      if (!isApiError(error))
        setNativeError(error instanceof Error ? error : new Error('Passkey registration failed'))
    } finally {
      nativeFence.current = false
      setNativePending(false)
    }
  }

  const authenticateWebAuthn = async () => {
    if (blocked || nativeFence.current || !window.PublicKeyCredential || !navigator.credentials)
      return
    const generation = sensitiveGeneration.current
    nativeFence.current = true
    setNativePending(true)
    setNativeError(null)
    try {
      const options = await authenticateWebAuthnMutation.mutateAsync({ purpose: 'step_up' })
      if (generation !== sensitiveGeneration.current) return
      const response = await requestPasskeyAssertion(options)
      if (!response || generation !== sensitiveGeneration.current) return
      await verifyWebAuthnMutation.mutateAsync({ challengeId: options.challengeId, response })
    } catch (error) {
      if (!isApiError(error))
        setNativeError(error instanceof Error ? error : new Error('Passkey verification failed'))
    } finally {
      nativeFence.current = false
      setNativePending(false)
    }
  }

  const security = readableQueryData(securityQuery)
  const principal = security?.principal

  if (securityQuery.isLoading) {
    return (
      <div className="soha-provider-portal is-loading">
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div className="soha-page soha-provider-portal is-app-shell">
      <div className="soha-portal-detail-shell">
        <ReadFeedback query={securityQuery} label="账户安全" />
        {!readDenied ? (
          <>
            {revokeMutation.feedback}
            {beginTOTPMutation.feedback}
            {beginRecoveryMutation.feedback}
            {beginWebAuthnMutation.feedback}
            {authenticateWebAuthnMutation.feedback}
            {verifyWebAuthnMutation.feedback}
            {recoveryMutation.feedback}
            {!totpChallenge && !recoveryChallenge ? verifyMutation.feedback : null}
            <OperationFeedback
              label="浏览器 Passkey 验证"
              pending={nativePending}
              error={nativeError}
              readOnly
            />
          </>
        ) : null}

        {security ? (
          <>
            <div className="soha-portal-detail-hero-bezel">
              <header className="soha-portal-detail-hero">
                <div className="soha-portal-detail-hero__identity">
                  <div className="soha-portal-principal is-hero-inline">
                    <PortalUserAvatar size={56} />
                  </div>
                  <div className="soha-portal-detail-hero__copy">
                    <div className="soha-portal-detail-hero__title-row">
                      <Title level={3}>{principal?.userName || 'User'}</Title>
                      <Tag color={security.mfaEnabled ? 'success' : 'default'}>
                        {security.mfaEnabled ? 'MFA Enabled' : 'MFA Not enabled'}
                      </Tag>
                    </div>
                    <Text type="secondary" ellipsis title={principal?.email}>
                      {principal?.email || principal?.userId || '-'}
                    </Text>
                  </div>
                </div>

                <Space className="soha-portal-detail-hero__actions" size={8} wrap>
                  <Tooltip title={localeCode === 'en_US' ? 'Refresh' : '刷新安全记录'}>
                    <Button
                      aria-label="刷新安全记录"
                      icon={<ReloadOutlined />}
                      loading={securityQuery.isFetching || credentialsQuery.isFetching}
                      onClick={() =>
                        void Promise.all([
                          securityQuery.refetch(),
                          runtimeQuery.refetch(),
                          credentialsQuery.isEnabled ? credentialsQuery.refetch() : undefined,
                        ])
                      }
                    />
                  </Tooltip>
                  <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/portal')}>
                    {localeCode === 'en_US' ? 'Back to Portal' : '返回门户'}
                  </Button>
                </Space>
              </header>
            </div>

            <section className="soha-portal-security-kpi-strip" aria-label="安全态势摘要">
              <div className="soha-portal-security-kpi-tile">
                <span className="soha-portal-security-kpi-tile__label">
                  <LockOutlined />
                  <span>MFA</span>
                </span>
                <strong>{security.mfaEnabled ? 'Enabled' : 'Not enabled'}</strong>
              </div>
              <div className="soha-portal-security-kpi-tile">
                <span className="soha-portal-security-kpi-tile__label">
                  <SafetyCertificateOutlined />
                  <span>Active sessions</span>
                </span>
                <strong>{security.activeSession ?? '-'}</strong>
              </div>
              <div className="soha-portal-security-kpi-tile">
                <span className="soha-portal-security-kpi-tile__label">
                  <KeyOutlined />
                  <span>Linked sources</span>
                </span>
                <strong>{security.linkedSources.length ?? '-'}</strong>
              </div>
              <div className="soha-portal-security-kpi-tile">
                <span className="soha-portal-security-kpi-tile__label">
                  <ClockCircleOutlined />
                  <span>Recent login</span>
                </span>
                <strong>{formatPortalDateTime(security.recentLoginAt)}</strong>
              </div>
            </section>

            <section className="soha-portal-detail-layout">
              <div className="soha-portal-detail-main">
                <section className="soha-portal-side-panel soha-portal-detail-card">
                  <div className="soha-portal-side-panel-head">
                    <div className="soha-portal-side-title">
                      <span className="soha-portal-detail-section-icon">
                        <LockOutlined />
                      </span>
                      <Title level={4} className="soha-portal-detail-card-title">
                        Multi-factor authentication
                      </Title>
                    </div>
                  </div>
                  <div className="soha-portal-security-card-body">
                    <ReadFeedback query={runtimeQuery} label="MFA 运行能力" />
                    {runtimeQuery.isPending ? (
                      <Spin size="small" />
                    ) : !runtime ? null : !mfaAvailable ? (
                      <ContextFeedback
                        showIcon
                        title="MFA runtime unavailable"
                        description="This server has no usable MFA challenge runtime."
                        type="warning"
                      />
                    ) : (
                      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                        <Space wrap>
                          <Button
                            disabled={blocked || !runtime?.totp?.available}
                            loading={beginTOTPMutation.isPending}
                            onClick={() => beginTOTPMutation.mutate(undefined)}
                          >
                            Add authenticator
                          </Button>
                          <Button
                            disabled={
                              blocked ||
                              !runtime?.webauthn?.available ||
                              !window.PublicKeyCredential ||
                              !navigator.credentials
                            }
                            loading={
                              beginWebAuthnMutation.isPending || verifyWebAuthnMutation.isPending
                            }
                            onClick={() => void enrollWebAuthn()}
                          >
                            Add passkey
                          </Button>
                          <Button
                            disabled={
                              blocked ||
                              !runtime?.webauthn?.available ||
                              !runtime?.stepUp?.available ||
                              !window.PublicKeyCredential ||
                              !navigator.credentials
                            }
                            loading={
                              authenticateWebAuthnMutation.isPending ||
                              verifyWebAuthnMutation.isPending
                            }
                            onClick={() => void authenticateWebAuthn()}
                          >
                            Verify passkey
                          </Button>
                          <Popconfirm
                            title="Generate new recovery codes?"
                            description="Previously generated recovery codes will stop working."
                            onConfirm={() => recoveryMutation.mutate(undefined)}
                          >
                            <Button
                              disabled={blocked || !runtime?.recoveryCodes?.available}
                              loading={recoveryMutation.isPending}
                            >
                              Recovery codes
                            </Button>
                          </Popconfirm>
                          <Button
                            disabled={blocked || !runtime?.recoveryCodes?.available}
                            icon={<KeyOutlined />}
                            loading={beginRecoveryMutation.isPending}
                            onClick={() => beginRecoveryMutation.mutate(undefined)}
                          >
                            Use recovery code
                          </Button>
                        </Space>
                        <ReadFeedback
                          query={credentialsQuery}
                          label="MFA 凭据"
                          canRead={credentialsQuery.isEnabled}
                        />
                        {credentialsQuery.isLoading ? (
                          <Spin size="small" />
                        ) : credentialsQuery.isError && !credentials ? null : credentials?.length ? (
                          <ul className="soha-portal-security-cred-list">
                            {credentials.map((credential) => (
                              <li key={credential.id} className="soha-portal-security-cred-item">
                                <div className="soha-portal-security-cred-meta">
                                  <Text strong>{credential.displayName}</Text>
                                  <Text type="secondary">
                                    {`${credential.type} · ${formatPortalDateTime(credential.lastUsedAt || credential.createdAt)}`}
                                  </Text>
                                </div>
                                <Popconfirm
                                  title="Revoke this credential?"
                                  onConfirm={() => revokeMutation.mutate(credential.id)}
                                >
                                  <Button danger size="small" disabled={blocked}>
                                    Revoke
                                  </Button>
                                </Popconfirm>
                              </li>
                            ))}
                          </ul>
                        ) : (
                          <Empty
                            image={Empty.PRESENTED_IMAGE_SIMPLE}
                            description="No MFA credentials"
                          />
                        )}
                      </Space>
                    )}
                  </div>
                </section>

                <section className="soha-portal-side-panel soha-portal-detail-card is-grow">
                  <div className="soha-portal-side-panel-head">
                    <div className="soha-portal-side-title">
                      <span className="soha-portal-detail-section-icon">
                        <SafetyCertificateOutlined />
                      </span>
                      <Title level={4} className="soha-portal-detail-card-title">
                        Principal identity
                      </Title>
                    </div>
                  </div>
                  <div className="soha-portal-security-fact-grid">
                    <div className="soha-portal-security-fact-tile">
                      <span>User ID</span>
                      <strong>{principal?.userId || '-'}</strong>
                    </div>
                    <div className="soha-portal-security-fact-tile">
                      <span>Email</span>
                      <strong>{principal?.email || '-'}</strong>
                    </div>
                    <div className="soha-portal-security-fact-tile">
                      <span>MFA</span>
                      <strong>{security.mfaEnabled ? 'Enabled' : 'Not enabled'}</strong>
                    </div>
                    <div className="soha-portal-security-fact-tile">
                      <span>Recent login</span>
                      <strong>{formatPortalDateTime(security.recentLoginAt)}</strong>
                    </div>
                  </div>
                </section>
              </div>

              <aside className="soha-portal-side">
                <section className="soha-portal-side-panel soha-portal-detail-card is-grow">
                  <div className="soha-portal-side-panel-head">
                    <div className="soha-portal-side-title">
                      <span className="soha-portal-detail-section-icon">
                        <TeamOutlined />
                      </span>
                      <Title level={4} className="soha-portal-detail-card-title">
                        Sources & access memberships
                      </Title>
                    </div>
                  </div>
                  <div className="soha-portal-security-side-groups">
                    <div className="soha-portal-security-tag-block">
                      <span className="soha-portal-security-tag-label">
                        <KeyOutlined />
                        <span>Linked sources</span>
                      </span>
                      <div className="soha-portal-app-tags">
                        <PortalTagsOrEmpty values={security.linkedSources} />
                      </div>
                    </div>

                    <div className="soha-portal-security-tag-block">
                      <span className="soha-portal-security-tag-label">
                        <TeamOutlined />
                        <span>Roles</span>
                      </span>
                      <div className="soha-portal-app-tags">
                        <PortalTagsOrEmpty values={principal?.roles} />
                      </div>
                    </div>

                    <div className="soha-portal-security-tag-block">
                      <span className="soha-portal-security-tag-label">
                        <TeamOutlined />
                        <span>Teams</span>
                      </span>
                      <div className="soha-portal-app-tags">
                        <PortalTagsOrEmpty values={principal?.teams} />
                      </div>
                    </div>

                    <div className="soha-portal-security-tag-block">
                      <span className="soha-portal-security-tag-label">
                        <InfoCircleOutlined />
                        <span>Tags</span>
                      </span>
                      <div className="soha-portal-app-tags">
                        <PortalTagsOrEmpty values={principal?.tags} />
                      </div>
                    </div>
                  </div>
                </section>
              </aside>
            </section>
          </>
        ) : null}
      </div>

      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: false }}
        cancelButtonProps={{ disabled: blocked }}
        destroyOnHidden
        okButtonProps={{
          disabled: blocked || totpCode.trim().length < 6,
          loading: verifyMutation.isPending,
        }}
        onCancel={() => {
          if (blocked) return
          setTOTPChallenge(null)
          setTOTPCode('')
        }}
        onOk={() =>
          totpChallenge &&
          verifyMutation.mutate(
            { challengeId: totpChallenge.challengeId, response: totpCode.trim() },
            {
              onSuccess: () => {
                setTOTPChallenge(null)
                setTOTPCode('')
                void credentialsQuery.refetch()
              },
            },
          )
        }
        open={!denied && Boolean(totpChallenge)}
        title="Register authenticator"
      >
        {verifyMutation.feedback}
        <ContextFeedback
          showIcon
          title="Scan this provisioning URI in your authenticator app"
          description={<Text copyable>{totpChallenge?.provisioningUri}</Text>}
          type="info"
        />
        <Input.OTP
          disabled={blocked}
          length={6}
          onChange={setTOTPCode}
          value={totpCode}
          style={{ marginTop: 16 }}
        />
      </Modal>

      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: false }}
        cancelButtonProps={{ disabled: blocked }}
        destroyOnHidden
        okButtonProps={{
          disabled: blocked || recoveryCode.trim().length < 16,
          loading: verifyMutation.isPending,
        }}
        onCancel={() => {
          if (blocked) return
          setRecoveryChallenge(null)
          setRecoveryCode('')
        }}
        onOk={() =>
          recoveryChallenge &&
          verifyMutation.mutate(
            { challengeId: recoveryChallenge.challengeId, response: recoveryCode.trim() },
            {
              onSuccess: () => {
                setRecoveryChallenge(null)
                setRecoveryCode('')
              },
            },
          )
        }
        open={!denied && Boolean(recoveryChallenge)}
        title="Use recovery code"
      >
        {verifyMutation.feedback}
        <Input
          disabled={blocked}
          autoComplete="one-time-code"
          maxLength={17}
          onChange={(event) => setRecoveryCode(event.target.value)}
          placeholder="XXXXXXXX-XXXXXXXX"
          value={recoveryCode}
        />
      </Modal>

      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        cancelButtonProps={{ style: { display: 'none' } }}
        destroyOnHidden
        onOk={() => setRecoveryCodes([])}
        onCancel={() => setRecoveryCodes([])}
        open={!denied && recoveryCodes.length > 0}
        title="Recovery codes"
      >
        <ContextFeedback
          showIcon
          title="Shown once"
          description="Store these codes securely. Each code can be used only once."
          type="warning"
        />
        <ul className="soha-list-panel">
          {recoveryCodes.map((code) => (
            <li key={code} className="soha-list-row">
              <Text code copyable>
                {code}
              </Text>
            </li>
          ))}
        </ul>
      </Modal>
    </div>
  )
}
