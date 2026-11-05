import { MetadataTag } from '@/components/status-tag'
import { ConfigurationYAMLDetailPage } from '../shared/yaml-detail'
import { PriorityClassOverview } from './overview'
import { priorityClassFromYAML } from './priority-model'
import './styles.css'
export function ConfigurationPriorityClassDetailPage() {
  return (
    <ConfigurationYAMLDetailPage
      kind="priorityclasses"
      label="PriorityClass"
      className="soha-priority-detail"
      fromYAML={priorityClassFromYAML}
      renderFacts={(detail, zh) => (
        <>
          <span>
            {zh ? '优先级 ' : 'Priority '}
            {new Intl.NumberFormat('en-US').format(detail.value)}
          </span>
          {detail.globalDefault ? (
            <MetadataTag tone="blue" label={zh ? '全局默认' : 'Global default'} />
          ) : null}
          <MetadataTag label={detail.preemptionPolicy} />
        </>
      )}
      renderOverview={(detail) => <PriorityClassOverview detail={detail} />}
      deleteDescription={[
        '删除此优先级类不会删除已有 Pod；后续新 Pod 将无法再引用它。',
        'This deletes the priority class, not existing Pods. New Pods will no longer be able to reference it.',
      ]}
    />
  )
}
