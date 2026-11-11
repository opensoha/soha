import { ConfigurationYAMLDetailPage } from '../shared/yaml-detail'
import { leaseFromYAML } from './lease-model'
import { LeaseOverview } from './overview'
import './styles.css'

export function ConfigurationLeaseDetailPage() {
  return (
    <ConfigurationYAMLDetailPage
      kind="leases"
      label="Lease"
      className="soha-lease-detail"
      fromYAML={leaseFromYAML}
      renderFacts={(detail, zh) => (
        <span>
          {detail.leaseDurationSeconds == null
            ? zh
              ? '未声明租期'
              : 'Duration not declared'
            : `${zh ? '租期' : 'Duration'} ${detail.leaseDurationSeconds} s`}
        </span>
      )}
      renderOverview={(detail) => <LeaseOverview detail={detail} />}
      deleteDescription={[
        '此 Lease 可能用于节点心跳或选主。删除会移除协调记录，可能触发重新选主；不会停止原持有者。',
        'This Lease may coordinate node heartbeats or leader election. Deleting the record may trigger re-election; it does not stop its previous holder.',
      ]}
    />
  )
}
