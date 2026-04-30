import { HomeOutlined, ReloadOutlined } from '@ant-design/icons'
import { history } from '@umijs/max'
import { Button, Result, Space } from 'antd'
import { findFirstAccessiblePath } from '@/routes/meta'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'

export default function Exception404Page() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const fallbackPath = findFirstAccessiblePath(permissionSnapshotQuery.data?.data) ?? '/'

  return (
    <div className="flex min-h-[calc(100vh-112px)] items-center justify-center px-6">
      <Result
        status="404"
        title="页面不存在"
        subTitle="当前地址没有对应的控制台页面，或者该页面在 simple 精简模式下未启用。"
        extra={(
          <Space wrap>
            <Button
              type="primary"
              icon={<HomeOutlined />}
              onClick={() => history.push(fallbackPath)}
            >
              返回工作台
            </Button>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => history.push(history.location.pathname)}
            >
              重新加载当前地址
            </Button>
          </Space>
        )}
      />
    </div>
  )
}
