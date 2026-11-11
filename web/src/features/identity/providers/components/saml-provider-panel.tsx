import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { SafetyCertificateOutlined } from '@ant-design/icons'
import { useQueryClient } from '@tanstack/react-query'
import { Button, Descriptions, Popconfirm, Select, Space } from 'antd'
import { useState } from 'react'
import { identityProviderMutations } from '../mutations'
import type { IdentityProvider } from '../types'
export function SAMLProviderPanel({
  canRotate,
  provider,
}: {
  canRotate: boolean
  provider: IdentityProvider
}) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [overlapSeconds, setOverlapSeconds] = useState(604800)
  const rotateMutation = useOperationMutation(
    identityProviderMutations.rotateSAMLCertificate(queryClient),
    {
      label: ['轮换SAML证书 认证接入', 'Rotate SAML certificate provider'],
      target: (v) => v.providerId,
    },
  )
  const operationBlocked = rotateMutation.blocked
  const config = provider.config ?? {}
  const stringValue = (key: string) => (typeof config[key] === 'string' ? config[key] : '-')
  const listValue = (key: string) =>
    Array.isArray(config[key]) ? (config[key] as unknown[]).join(', ') || '-' : '-'
  return (
    <>
      {rotateMutation.feedback}
      <Descriptions
        bordered
        column={{ xs: 1, md: 2 }}
        items={[
          { key: 'entityId', label: 'Entity ID', children: stringValue('entityId') },
          { key: 'nameId', label: 'NameID', children: stringValue('nameIdFormat') },
          {
            key: 'acs',
            label: 'ACS URLs',
            children: listValue(
              Array.isArray(config.assertionConsumerServiceUrls)
                ? 'assertionConsumerServiceUrls'
                : 'acsUrls',
            ),
            span: { xs: 1, md: 2 },
          },
          { key: 'audience', label: 'Audience', children: stringValue('audience') },
          { key: 'recipient', label: 'Recipient', children: stringValue('recipient') },
          {
            key: 'certificate',
            label: t('identity.providers.samlCertificate', 'SAML 证书'),
            children: (
              <Space wrap>
                <Select
                  disabled={operationBlocked}
                  aria-label={t('identity.providers.samlOverlap', '证书重叠期')}
                  onChange={setOverlapSeconds}
                  options={[
                    { label: t('identity.providers.samlOverlapNone', '不重叠'), value: 0 },
                    { label: t('identity.providers.samlOverlapOneDay', '1 天'), value: 86400 },
                    { label: t('identity.providers.samlOverlapSevenDays', '7 天'), value: 604800 },
                    {
                      label: t('identity.providers.samlOverlapThirtyDays', '30 天'),
                      value: 2592000,
                    },
                  ]}
                  style={{ width: 120 }}
                  value={overlapSeconds}
                />
                <Popconfirm
                  cancelText={t('common.cancel', '取消')}
                  disabled={operationBlocked || !canRotate}
                  okButtonProps={{
                    ...{ loading: rotateMutation.isPending },
                    disabled: operationBlocked,
                  }}
                  okText={t('common.confirm', '确认')}
                  onConfirm={() =>
                    rotateMutation.mutate({ providerId: provider.id, input: { overlapSeconds } })
                  }
                  title={t(
                    'identity.providers.samlCertificateRotateConfirm',
                    '确认轮换 SAML 签名证书？',
                  )}
                >
                  <Button
                    disabled={!canRotate}
                    icon={<SafetyCertificateOutlined />}
                    loading={rotateMutation.isPending}
                    size="small"
                  >
                    {t('identity.providers.samlCertificateRotate', '轮换证书')}
                  </Button>
                </Popconfirm>
              </Space>
            ),
            span: { xs: 1, md: 2 },
          },
        ]}
        size="small"
      />
    </>
  )
}
