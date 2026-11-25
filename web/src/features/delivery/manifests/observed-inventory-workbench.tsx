import { Typography } from 'antd'
import type { ManifestDeployment } from '@opensoha/contracts/gen/ts/sohaapi'
import { ManagementState } from '@/components/management-list'
import { ResourceRelationWorkbench } from '../runtime/resource-relation-workbench'
import { projectObservedManifestInventory } from './observed-inventory-relations'

export function ObservedManifestInventoryWorkbench({
  deployment,
  scope,
}: {
  deployment: ManifestDeployment
  scope?: { clusterId: string; namespace: string }
}) {
  const data = scope && projectObservedManifestInventory(deployment, scope)
  return (
    <section aria-label="已部署清单关系" className="soha-service-runtime-overview">
      <div className="soha-service-section-heading">
        <h2>已部署清单关系</h2>
      </div>
      {data ? (
        <>
          <Typography.Text type="secondary">按已观察的部署清单查看成员和资源身份。</Typography.Text>
          <ResourceRelationWorkbench data={data} manifestInventory renderActions={() => null} />
        </>
      ) : (
        <ManagementState
          compact
          bordered={false}
          kind="not-configured"
          title="暂无可确认的已部署清单资源"
        />
      )}
    </section>
  )
}
