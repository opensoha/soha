import { ConfigurationYAMLDetailPage } from '../shared/yaml-detail'
import { RuntimeClassOverview } from './overview'
import { runtimeClassFromYAML } from './runtime-model'
import './styles.css'
export function ConfigurationRuntimeClassDetailPage() {
  return (
    <ConfigurationYAMLDetailPage
      kind="runtimeclasses"
      label="RuntimeClass"
      className="soha-runtime-detail"
      fromYAML={runtimeClassFromYAML}
      renderFacts={(detail) => <span>Handler · {detail.handler}</span>}
      renderOverview={(detail) => <RuntimeClassOverview detail={detail} />}
      deleteDescription={[
        '删除此运行时类不会删除已有 Pod；后续新 Pod 将无法再引用它。',
        'This deletes the runtime class, not existing Pods. New Pods will no longer be able to reference it.',
      ]}
    />
  )
}
