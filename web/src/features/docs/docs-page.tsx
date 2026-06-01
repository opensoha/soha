import { Button } from 'antd'
import { ManagementDetailHeader } from '@/components/management-list'

export function DocsPage() {
  return (
    <div className="soha-page" style={{ minHeight: '100%' }}>
      <ManagementDetailHeader
        title="项目文档"
        description="在控制台内嵌浏览项目文档，也可以在独立窗口中直接打开文档站。"
        actions={
          <Button type="primary" variant="outlined" onClick={() => window.open('/docs/', '_blank', 'noopener,noreferrer')}>
            在新窗口打开
          </Button>
        }
      />
      <div style={{ flex: 1, minHeight: 0 }}>
        <iframe
          src="/docs/"
          title="Soha Documentation"
          className="w-full border-0"
          style={{ minHeight: 'calc(100vh - 210px)' }}
        />
      </div>
    </div>
  )
}
