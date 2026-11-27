import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback, useInlineApiError } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import type { IdentityApplication } from '@/features/identity'
import { useI18n } from '@/i18n'
import { isApiError } from '@/services/api-error'
import { readableQueryData } from '@/utils/query-feedback'
import {
  AppstoreOutlined,
  ArrowLeftOutlined,
  KeyOutlined,
  LinkOutlined,
  ReloadOutlined,
  StarFilled,
  StarOutlined,
  TagsOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Descriptions, Result, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { providerPortalMutations } from '../mutations'
import '../provider-portal-pages.css'
import { providerPortalQueries } from '../queries'
import {
  PortalApplicationAvatar,
  PortalTagsOrEmpty,
  portalProviderLabels,
  portalStatusLabels,
} from '../shared/application-ui'
import { formatPortalDateTime, portalMetadataValue } from '../shared/formatters'

const { Paragraph, Text, Title } = Typography

export function PortalApplicationDetailPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const { applicationId = '' } = useParams()
  const queryClient = useQueryClient()

  const applicationQuery = useQuery(providerPortalQueries.application(applicationId))
  const launchMutation = useOperationMutation(providerPortalMutations.launch(queryClient), {
    label: ['打开应用', 'Open application'],
    target: (v) => v.name,
    resultError: (decision) =>
      decision.launchUrl
        ? undefined
        : new Error(
            t(
              'providerPortal.home.launchUrlNotConfigured',
              'Application launch URL is not configured',
            ),
          ),
    receipt: () => ({
      message: '已收到应用入口',
      evidence: '目标系统是否登录成功由目标系统确认。',
    }),
  })
  const favoriteMutation = useOperationMutation(
    providerPortalMutations.toggleFavorite(queryClient),
    {
      label: ['更新应用收藏', 'Update favorite'],
      target: (v) => v.name,
      receipt: (_, v) => ({ message: v.favorite ? '已取消收藏' : '已收藏' }),
    },
  )

  const launchApplication = (application: IdentityApplication) => {
    launchMutation.mutate(application, {
      onSuccess: (decision) => {
        if (decision.launchUrl) window.location.assign(decision.launchUrl)
      },
    })
  }

  const application = readableQueryData(applicationQuery)
  const notFound = isApiError(applicationQuery.error) && applicationQuery.error.status === 404
  useInlineApiError(notFound ? applicationQuery.error : null)
  const blocked = launchMutation.blocked || favoriteMutation.blocked
  const status = application
    ? (portalStatusLabels[application.status] ?? portalStatusLabels.draft)
    : portalStatusLabels.draft
  const statusLabel = t(
    `providerPortal.application.status.${application?.status ?? 'draft'}`,
    status.label,
  )
  const providerLabel = application
    ? t(
        `providerPortal.application.provider.${application.providerType}`,
        portalProviderLabels[application.providerType] ?? application.providerType,
      )
    : '-'
  const favoriteLabel = application?.favorite
    ? t('providerPortal.home.unfavorite', 'Unfavorite')
    : t('providerPortal.home.favorite', 'Favorite')
  const metadataEntries = Object.entries(application?.metadata ?? {}).slice(0, 8)

  if (applicationQuery.isLoading) {
    return (
      <div className="soha-provider-portal is-loading">
        <Spin size="large" />
      </div>
    )
  }

  if (!application) {
    return (
      <div className="soha-page soha-provider-portal is-app-shell">
        <div className="soha-portal-detail-shell">
          {applicationQuery.isError && !notFound ? (
            <ReadFeedback query={applicationQuery} label="门户应用" />
          ) : (
            <div className="soha-portal-detail-unavailable">
              <Result
                status="404"
                title={t('providerPortal.detail.unavailable', 'Application not available')}
                subTitle={t(
                  'providerPortal.detail.unavailableDescription',
                  'The application is disabled, hidden, or not assigned to your identity.',
                )}
                extra={
                  <Button
                    icon={<ArrowLeftOutlined />}
                    type="primary"
                    onClick={() => navigate('/portal')}
                  >
                    {t('providerPortal.detail.backToPortal', 'Back to Portal')}
                  </Button>
                }
              />
            </div>
          )}
          {applicationQuery.isError && !notFound ? (
            <Button onClick={() => navigate('/portal')}>
              {t('providerPortal.detail.backToPortal', 'Back to Portal')}
            </Button>
          ) : null}
        </div>
      </div>
    )
  }

  return (
    <div className="soha-page soha-provider-portal is-app-shell">
      <div className="soha-portal-detail-shell">
        <ReadFeedback query={applicationQuery} label="门户应用" />
        {launchMutation.feedback}
        {favoriteMutation.feedback}

        <div className="soha-portal-detail-hero-bezel">
          <header className="soha-portal-detail-hero">
            <div className="soha-portal-detail-hero__identity">
              <span className="soha-portal-app-icon is-detail-hero">
                <PortalApplicationAvatar application={application} />
                {application.favorite ? (
                  <span className="soha-portal-app-fav-badge" aria-hidden="true">
                    <StarFilled />
                  </span>
                ) : null}
              </span>
              <div className="soha-portal-detail-hero__copy">
                <div className="soha-portal-detail-hero__title-row">
                  <Title level={3}>{application.name}</Title>
                  <Space size={[6, 6]} wrap>
                    <Tag color={status.color}>{statusLabel}</Tag>
                    <Tag>{providerLabel}</Tag>
                    {application.featured ? (
                      <Tag color="blue">{t('providerPortal.home.featured', 'Featured')}</Tag>
                    ) : null}
                  </Space>
                </div>
                <Paragraph className="soha-portal-detail-hero__desc" type="secondary">
                  {application.description ||
                    t('providerPortal.home.noDescription', 'No description')}
                </Paragraph>
              </div>
            </div>

            <Space className="soha-portal-detail-hero__actions" size={8} wrap>
              <Button
                aria-label="刷新应用详情"
                icon={<ReloadOutlined />}
                loading={applicationQuery.isFetching}
                onClick={() => void applicationQuery.refetch()}
              />
              <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/portal')}>
                {t('providerPortal.detail.backToPortal', 'Back to Portal')}
              </Button>
              <Tooltip title={favoriteLabel}>
                <Button
                  icon={application.favorite ? <StarFilled /> : <StarOutlined />}
                  loading={favoriteMutation.isPending}
                  disabled={blocked}
                  aria-label={favoriteLabel}
                  onClick={() => favoriteMutation.mutate(application)}
                >
                  {application.favorite
                    ? t('providerPortal.detail.favorited', 'Favorited')
                    : favoriteLabel}
                </Button>
              </Tooltip>
              <Button
                disabled={blocked || application.status !== 'enabled'}
                icon={<LinkOutlined />}
                loading={launchMutation.isPending}
                type="primary"
                onClick={() => launchApplication(application)}
              >
                {t('providerPortal.home.open', 'Open')}
              </Button>
            </Space>
          </header>
        </div>

        <section className="soha-portal-detail-layout">
          <div className="soha-portal-detail-main">
            <section className="soha-portal-side-panel soha-portal-detail-card">
              <div className="soha-portal-side-panel-head">
                <div className="soha-portal-side-title">
                  <span className="soha-portal-detail-section-icon">
                    <AppstoreOutlined />
                  </span>
                  <Title level={4} className="soha-portal-detail-card-title">
                    {t('providerPortal.detail.application', 'Application')}
                  </Title>
                </div>
              </div>
              <Descriptions
                className="soha-portal-detail-descriptions"
                column={{ xs: 1, sm: 2 }}
                size="small"
                items={[
                  {
                    key: 'slug',
                    label: t('providerPortal.detail.slug', 'Slug'),
                    children: <Text code>{application.slug}</Text>,
                  },
                  {
                    key: 'status',
                    label: t('common.status', 'Status'),
                    children: <Tag color={status.color}>{statusLabel}</Tag>,
                  },
                  {
                    key: 'providerType',
                    label: t('identity.applications.providerType', 'Provider type'),
                    children: providerLabel,
                  },
                  {
                    key: 'providerId',
                    label: t('identity.applications.providerId', 'Provider ID'),
                    children: application.providerId || '-',
                  },
                  {
                    key: 'createdAt',
                    label: t('common.createdAt', 'Created At'),
                    children: formatPortalDateTime(application.createdAt, localeCode),
                  },
                  {
                    key: 'updatedAt',
                    label: t('common.updatedAt', 'Updated At'),
                    children: formatPortalDateTime(application.updatedAt, localeCode),
                  },
                  {
                    key: 'lastLaunchedAt',
                    label: t('providerPortal.detail.lastLaunch', 'Last launch'),
                    children: formatPortalDateTime(application.lastLaunchedAt, localeCode),
                    span: 2,
                  },
                ]}
              />
            </section>

            <section className="soha-portal-side-panel soha-portal-detail-card">
              <div className="soha-portal-side-panel-head">
                <div className="soha-portal-side-title">
                  <span className="soha-portal-detail-section-icon">
                    <LinkOutlined />
                  </span>
                  <span>{t('identity.applications.launchUrl', 'Launch URL')}</span>
                </div>
              </div>
              {application.launchUrl ? (
                <div className="soha-portal-url-capsule">
                  <Text className="soha-portal-url" copyable>
                    {application.launchUrl}
                  </Text>
                </div>
              ) : (
                <ContextFeedback
                  showIcon
                  type="warning"
                  title={t(
                    'providerPortal.home.launchUrlNotConfigured',
                    'Application launch URL is not configured',
                  )}
                />
              )}
            </section>
          </div>

          <aside className="soha-portal-side soha-portal-detail-side">
            <section className="soha-portal-side-panel soha-portal-detail-card">
              <div className="soha-portal-side-panel-head">
                <div className="soha-portal-side-title">
                  <span className="soha-portal-detail-section-icon">
                    <TagsOutlined />
                  </span>
                  <span>{t('providerPortal.home.tags', 'Tags')}</span>
                </div>
              </div>
              <div className="soha-portal-app-tags">
                <PortalTagsOrEmpty values={application.tags} />
              </div>
            </section>

            <section className="soha-portal-side-panel soha-portal-detail-card is-stretch">
              <div className="soha-portal-side-panel-head">
                <div className="soha-portal-side-title">
                  <span className="soha-portal-detail-section-icon">
                    <KeyOutlined />
                  </span>
                  <span>{t('providerPortal.detail.metadata', 'Metadata')}</span>
                </div>
              </div>
              {metadataEntries.length ? (
                <div className="soha-portal-metadata-list">
                  {metadataEntries.map(([key, value]) => (
                    <div className="soha-portal-metadata-row" key={key}>
                      <Text type="secondary">{key}</Text>
                      <Text strong>{portalMetadataValue(value)}</Text>
                    </div>
                  ))}
                </div>
              ) : (
                <Text type="secondary">{t('providerPortal.detail.noMetadata', 'No metadata')}</Text>
              )}
            </section>
          </aside>
        </section>
      </div>
    </div>
  )
}
