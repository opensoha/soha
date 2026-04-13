import { Button } from '@douyinfe/semi-ui'
import { PageHeader } from '@/components/page-header'

export function DocsPage() {
  return (
    <div className="kc-page" style={{ minHeight: '100%' }}>
      <PageHeader
        title="项目文档"
        description="在控制台内嵌浏览项目文档，也可以在独立窗口中直接打开文档站。"
        actions={
          <Button theme="light" type="primary" onClick={() => window.open('/docs/', '_blank', 'noopener,noreferrer')}>
            在新窗口打开
          </Button>
        }
      />
      <div style={{ flex: 1, minHeight: 0 }}>
        <iframe
          src="/docs/"
          title="KubeCrux Documentation"
          className="w-full border-0"
          style={{ minHeight: 'calc(100vh - 210px)' }}
        />
      </div>
    </div>
  )
}
