import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { AppstoreOutlined, UserOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Avatar, Button, Card, Spin, Typography } from 'antd'
import { useParams } from 'react-router-dom'
import { completeBrowserHandoff, inspectBrowserHandoff } from './browser-handoff-api'
import './browser-handoff-page.css'
import { authKeys } from './keys'
import { appBrowserPath } from '@/utils/app-base'

const { Paragraph, Text, Title } = Typography

function errorMessage(error: unknown) {
  return error instanceof Error && error.message
    ? error.message
    : '交接已过期或不可用，请从 Soha App 重新打开。'
}

export function BrowserHandoffPage({
  navigate = (destinationUrl: string) => window.location.assign(destinationUrl),
}: {
  navigate?: (destinationUrl: string) => void
}) {
  const { handoffId = '' } = useParams()
  const handoffQuery = useQuery({
    queryKey: authKeys.browserHandoff(handoffId),
    queryFn: () => inspectBrowserHandoff(handoffId),
    enabled: Boolean(handoffId),
    retry: false,
  })
  const completion = useOperationMutation(
    {
      mutationFn: () => completeBrowserHandoff(handoffId),
      onSuccess: ({ destinationUrl }) => {
        navigate(new URL(destinationUrl, window.location.origin).toString())
      },
    },
    { label: ['浏览器登录交接', 'Browser login handoff'], target: () => '当前登录交接' },
  )
  const handoff = readableQueryData(handoffQuery)

  return (
    <main className="soha-browser-handoff-page">
      <Card className="soha-browser-handoff-card" variant="outlined">
        <div className="soha-browser-handoff-brand">
          <img src={appBrowserPath('/logo.svg')} alt="" />
          <Text strong>Soha 浏览器登录</Text>
        </div>

        <ReadFeedback label="登录交接" query={handoffQuery} />
        {completion.feedback}
        {handoffQuery.isLoading ? (
          <div className="soha-browser-handoff-loading">
            <Spin description="正在检查安全交接" />
          </div>
        ) : handoffQuery.error || !handoff ? (
          <Alert
            showIcon
            type="error"
            title="无法继续"
            description={errorMessage(handoffQuery.error)}
          />
        ) : (
          <>
            <div className="soha-browser-handoff-heading">
              <Title level={3}>在浏览器中继续</Title>
              <Paragraph type="secondary">
                确认后将为浏览器创建独立会话，不会把 App 凭据交给网页。
              </Paragraph>
            </div>

            <div className="soha-browser-handoff-binding">
              <div>
                <Avatar icon={<UserOutlined />} />
                <span>
                  <Text strong>{handoff.accountName}</Text>
                  <Text type="secondary">Soha App 账号</Text>
                </span>
              </div>
              <div>
                <Avatar
                  shape="square"
                  src={handoff.application.iconUrl || undefined}
                  icon={handoff.application.iconUrl ? undefined : <AppstoreOutlined />}
                />
                <span>
                  <Text strong>{handoff.application.name}</Text>
                  <Text type="secondary">来自 Soha App</Text>
                </span>
              </div>
            </div>

            <div className="soha-browser-handoff-actions">
              <Button href={appBrowserPath('/')}>取消</Button>
              <Button
                type="primary"
                loading={completion.isPending}
                disabled={completion.blocked}
                onClick={() => completion.mutate()}
              >
                继续登录
              </Button>
            </div>
          </>
        )}
      </Card>
    </main>
  )
}
