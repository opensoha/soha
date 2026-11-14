import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { ApiError } from '@/services/api-client'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import type { UseQueryResult } from '@tanstack/react-query'
import { Card } from 'antd'
import { WorkloadOverviewConfiguration } from '../../workloads/shared/workload-overview'
import { SecretCertificateIdentityError } from './api'
import { certificateValidity } from './certificate-validity'
import type { SecretCertificate } from './types'

export function SecretCertificateCard({
  query,
}: {
  query: UseQueryResult<SecretCertificate, Error>
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const staleIdentity =
    query.error instanceof SecretCertificateIdentityError ||
    (query.error instanceof ApiError && query.error.status === 404)
  const certificate = staleIdentity ? undefined : readableQueryData(query)
  const validity = certificateValidity(certificate)
  const label =
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
    <Card title={zh ? 'TLS 证书' : 'TLS certificate'}>
      <ReadFeedback query={query} label={zh ? '证书信息' : 'Certificate metadata'} />
      {query.isPending ? <ManagementState kind="loading" compact bordered={false} /> : null}
      {certificate && validity.state !== 'unknown' ? (
        <>
          <div className="soha-secret-certificate-status">
            <StatusTag
              value={
                validity.state === 'valid'
                  ? 'healthy'
                  : validity.state === 'expired'
                    ? 'expired'
                    : 'warning'
              }
              label={label}
            />
            {certificate.selfSigned ? <MetadataTag label={zh ? '自签名' : 'Self-signed'} /> : null}
            {'days' in validity ? (
              <span>
                {zh ? '剩余 ' + validity.days + ' 天' : validity.days + ' days remaining'}
              </span>
            ) : null}
          </div>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '生效时间' : 'Valid from',
                children: formatDateTime(certificate.notBefore),
              },
              {
                label: zh ? '到期时间' : 'Expires at',
                children: formatDateTime(certificate.notAfter),
              },
            ]}
          />
          <p className="soha-secret-certificate-note">
            {zh ? '有效期不代表证书已受信任。' : 'Validity does not establish certificate trust.'}
          </p>
        </>
      ) : certificate ? (
        <ManagementState
          compact
          bordered={false}
          kind="empty"
          title={
            certificate.status === 'missing'
              ? zh
                ? '未找到 tls.crt'
                : 'tls.crt is missing'
              : certificate.status === 'invalid'
                ? zh
                  ? '证书无法解析'
                  : 'Certificate could not be parsed'
                : zh
                  ? '证书信息未提供'
                  : 'Certificate metadata unavailable'
          }
          description={
            zh ? '请检查此 Secret 的 TLS 配置。' : 'Check the TLS configuration of this Secret.'
          }
        />
      ) : null}
    </Card>
  )
}
