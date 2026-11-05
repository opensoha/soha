import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { ApiError } from '@/services/api-error'
import { InfoCircleOutlined } from '@ant-design/icons'
import type { UseQueryResult } from '@tanstack/react-query'
import { Button, Card, Descriptions, Space, Tooltip, Typography } from 'antd'
import type { ReactNode } from 'react'
import type { SecretCertificate } from '../../configuration/secrets'
import { SecretCertificateIdentityError } from '../../configuration/secrets'
import type { ingressConfiguration } from './configuration'
import { certificateValidity, MAX_INGRESS_CERTIFICATE_READS } from './certificate-validity'

type TLSReference = ReturnType<typeof ingressConfiguration>['tls'][number]
export function IngressTLSCard({
  tls,
  canRead,
  queries,
  reference,
}: {
  tls?: TLSReference[]
  canRead: boolean
  queries: ReadonlyMap<string, UseQueryResult<SecretCertificate, Error>>
  reference: (name: string) => ReactNode
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return (
    <Card
      className="soha-detail-card"
      title={
        <Space size={4}>
          {zh ? 'TLS 配置' : 'TLS configuration'}
          <Tooltip
            trigger={['hover', 'focus']}
            title={
              zh
                ? '有效期来自 Secret 中第一张证书；不代表证书受信任或入口 HTTPS 已生效。'
                : 'Dates describe the first certificate in the Secret, not certificate trust or deployed HTTPS.'
            }
          >
            <Button
              type="text"
              size="small"
              aria-label={zh ? '证书有效期说明' : 'Certificate validity information'}
              icon={<InfoCircleOutlined />}
            />
          </Tooltip>
        </Space>
      }
    >
      {!tls ? (
        <Typography.Text type="secondary">
          {zh
            ? 'TLS 引用未确认，请查看 YAML 读取状态'
            : 'TLS references unconfirmed; check YAML read status'}
        </Typography.Text>
      ) : !tls.length ? (
        <Typography.Text type="secondary">
          {zh ? '未配置 TLS' : 'TLS not configured'}
        </Typography.Text>
      ) : (
        <div className="soha-ingress-tls-list">
          {tls.map((item, index) => {
            const originalQuery = queries.get(item.secretName)
            const query =
              originalQuery?.error instanceof SecretCertificateIdentityError ||
              (originalQuery?.error instanceof ApiError && originalQuery.error.status === 404)
                ? { ...originalQuery, data: undefined }
                : originalQuery
            const certificate = canRead && query ? readableQueryData(query) : undefined
            const validity = certificateValidity(certificate)
            const unavailable =
              certificate?.status === 'missing'
                ? zh
                  ? 'Secret 缺少 tls.crt'
                  : 'Secret has no tls.crt'
                : certificate?.status === 'unsupported_type'
                  ? zh
                    ? '不是 TLS 类型 Secret'
                    : 'Secret is not a TLS Secret'
                  : certificate?.status === 'invalid'
                    ? zh
                      ? '证书无法解析'
                      : 'Certificate could not be parsed'
                    : zh
                      ? '证书有效期未知'
                      : 'Certificate validity unknown'
            const validityLabel =
              validity.state === 'expired'
                ? zh
                  ? '已过期'
                  : 'Expired'
                : validity.state === 'future'
                  ? zh
                    ? '尚未生效'
                    : 'Not yet valid'
                  : validity.state === 'expiring'
                    ? zh
                      ? '即将到期'
                      : 'Expiring soon'
                    : zh
                      ? '有效期内'
                      : 'Within validity period'
            return (
              <section
                key={`${item.secretName}/${index}`}
                className="soha-ingress-certificate"
                aria-label={
                  zh
                    ? `TLS 证书 ${item.secretName || index + 1}`
                    : `TLS certificate ${item.secretName || index + 1}`
                }
              >
                <div className="soha-ingress-certificate-heading">
                  <Space wrap size={6}>
                    <Typography.Text strong>
                      {item.hosts.join(', ') || (zh ? '未指定 Hosts' : 'Hosts not specified')}
                    </Typography.Text>
                    {certificate?.selfSigned && (
                      <MetadataTag label={zh ? '自签名' : 'Self-signed'} />
                    )}
                  </Space>
                  {validity.state !== 'unknown' && (
                    <Space wrap size={6}>
                      <StatusTag
                        value={
                          validity.state === 'valid'
                            ? 'healthy'
                            : validity.state === 'expired'
                              ? 'expired'
                              : 'warning'
                        }
                        label={validityLabel}
                      />
                      {'days' in validity && (
                        <Typography.Text type="secondary">
                          {zh ? `剩余 ${validity.days} 天` : `${validity.days} days remaining`}
                        </Typography.Text>
                      )}
                    </Space>
                  )}
                </div>
                <Typography.Text type="secondary">
                  Secret：
                  {item.secretName ? reference(item.secretName) : zh ? '未指定' : 'Not specified'}
                </Typography.Text>
                {!item.secretName ? (
                  <Typography.Text type="secondary">
                    {zh ? '未指定证书引用' : 'Certificate reference not specified'}
                  </Typography.Text>
                ) : !canRead ? (
                  <Typography.Text type="secondary">
                    {zh ? '无证书信息查看权限' : 'Certificate metadata access unavailable'}
                  </Typography.Text>
                ) : query ? (
                  <>
                    <ReadFeedback query={query} label={zh ? '证书信息' : 'Certificate metadata'} />
                    {query.isLoading ? (
                      <Typography.Text type="secondary">
                        {zh ? '正在读取证书…' : 'Reading certificate…'}
                      </Typography.Text>
                    ) : validity.state !== 'unknown' && certificate ? (
                      <Descriptions
                        size="small"
                        column={{ xs: 1, sm: 2 }}
                        items={[
                          {
                            key: 'start',
                            label: zh ? '生效时间' : 'Valid from',
                            children: formatDateTime(certificate.notBefore),
                          },
                          {
                            key: 'end',
                            label: zh ? '到期时间' : 'Expires at',
                            children: formatDateTime(certificate.notAfter),
                          },
                        ]}
                      />
                    ) : (
                      !query.isError && (
                        <Typography.Text type="secondary">{unavailable}</Typography.Text>
                      )
                    )}
                  </>
                ) : (
                  <Typography.Text type="secondary">
                    {zh
                      ? `其余证书信息未读取（本页最多 ${MAX_INGRESS_CERTIFICATE_READS} 个 Secret）`
                      : `Additional certificate metadata not read (limit: ${MAX_INGRESS_CERTIFICATE_READS} Secrets)`}
                  </Typography.Text>
                )}
              </section>
            )
          })}
        </div>
      )}
    </Card>
  )
}
