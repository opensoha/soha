import { useState } from 'react'
import { Alert, Space, Tabs } from 'antd'
import type { ManagedContentRecord } from '@opensoha/contracts/gen/ts/sohaapi'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { TransferContext, type TransferAccess } from './context'
import { TransferForm } from './transfer-form'
import { Records } from './records'
import { Destinations } from './destinations'

export default function ManagedContentPage() {
  const viewer = useAuthStore((s) => s.user?.userId ?? '')
  const permissions = usePermissionSnapshot()
  const ready = permissions.isSuccess && !permissions.isFetching && permissions.isEnabled !== false
  const snapshot = ready ? readableQueryData(permissions)?.data : undefined
  const access: TransferAccess = {
    view: hasPermission(snapshot, 'network_access.content_transfers.view'),
    create: hasPermission(snapshot, 'network_access.content_transfers.create'),
    download: hasPermission(snapshot, 'network_access.content_transfers.download'),
    remove: hasPermission(snapshot, 'network_access.content_transfers.delete'),
    destinationView: hasPermission(snapshot, 'network_access.content_destinations.view'),
    update: hasPermission(snapshot, 'network_access.content_destinations.update'),
    publish: hasPermission(snapshot, 'network_access.content_destinations.publish'),
    events: hasPermission(snapshot, 'network_access.content_transfer_events.view'),
    link: hasPermission(snapshot, 'network_access.dlp_linkage.manage'),
    review: hasPermission(snapshot, 'network_access.content_transfer_appeals.review'),
  }
  return (
    <Workspace key={`${viewer}:${Object.values(access).join()}`} viewer={viewer} access={access} />
  )
}
function Workspace({ viewer, access }: { viewer: string; access: TransferAccess }) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const [tab, setTab] = useState(
    access.view ? 'transfer' : access.destinationView ? 'destinations' : 'cases',
  )
  const [retryRecord, setRetryRecord] = useState<ManagedContentRecord>()
  const scope = Object.values(access).join()
  const resume = (record: ManagedContentRecord) => {
    setRetryRecord(record)
    setTab('transfer')
  }
  if (!access.view && !access.destinationView && !access.events)
    return (
      <Alert
        type="warning"
        showIcon
        title={text(
          '需要传输、目的连接或审查查看权限',
          'Transfer, destination or case viewing permission is required',
        )}
      />
    )
  return (
    <TransferContext.Provider value={{ viewer, scope, access, text, resume }}>
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          title={text(
            '经内容策略向批准目的地传输',
            'Transfer through content policy to approved destinations',
          )}
          description={text(
            '覆盖此入口的选文件、粘贴文本、文件下载与复制。当前完整文本检查上限为256 KiB；未检查不等于安全，审计模式仍可能放行。第三方网页、网盘客户端及其他应用入口不受此页控制。',
            'Covers file selection, pasted text, downloads and copies through this entry. Full text inspection is limited to 256 KiB. Uninspected does not mean safe; audit can still allow it. Third-party websites, drive clients and other application entry points are outside this scope.',
          )}
        />
        <Tabs
          activeKey={tab}
          onChange={setTab}
          items={[
            ...(access.view
              ? [
                  {
                    key: 'transfer',
                    label: text('传输', 'Transfer'),
                    children: (
                      <TransferForm
                        retryRecord={retryRecord}
                        clearRetry={() => setRetryRecord(undefined)}
                      />
                    ),
                  },
                  {
                    key: 'files',
                    label: text('我的文件', 'My files'),
                    children: <Records kind="files" />,
                  },
                  {
                    key: 'records',
                    label: text('我的记录', 'My records'),
                    children: <Records kind="records" />,
                  },
                ]
              : []),
            ...(access.destinationView
              ? [
                  {
                    key: 'destinations',
                    label: text('目的连接', 'Destinations'),
                    children: <Destinations />,
                  },
                ]
              : []),
            ...(access.events
              ? [
                  {
                    key: 'cases',
                    label: text('内容审查', 'Content review'),
                    children: <Records kind="cases" />,
                  },
                ]
              : []),
          ]}
        />
      </Space>
    </TransferContext.Provider>
  )
}
