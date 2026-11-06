import { Alert, Steps } from 'antd'
import { useI18n } from '@/i18n'
import type { PersistentVolumeClaimDetail } from '@/types/platform'
import { assessPersistentVolumeClaim } from './lifecycle-model'

export function PersistentVolumeClaimLifecyclePanel({
  claim,
}: {
  claim: PersistentVolumeClaimDetail
}) {
  const { localeCode } = useI18n()
  const assessment = assessPersistentVolumeClaim(claim)
  const messages = assessment.risks.map((risk) => {
    const zh = {
      lost: 'PVC 已进入 Lost 状态，请检查对应 PV 与存储后端。',
      pending: 'PVC 仍在等待绑定，请检查 StorageClass、容量和拓扑约束。',
      'multi-node-rwo':
        'ReadWriteOnce PVC 的 Pod 引用涉及多个节点，可检查调度与挂载事件；尚不能确认 Multi-Attach 故障。',
      truncated: 'Pod 引用已截断，仅展示当前返回范围。',
    }[risk]
    const en = {
      lost: 'The PVC is Lost; inspect the PV and storage backend.',
      pending: 'The PVC is waiting for binding; inspect StorageClass, capacity, and topology.',
      'multi-node-rwo':
        'ReadWriteOnce Pod references span multiple nodes. Inspect scheduling and mount events; this does not confirm a Multi-Attach failure.',
      truncated: 'Pod references are truncated; only the returned scope is shown.',
    }[risk]
    return localeCode === 'zh_CN' ? zh : en
  })

  return (
    <div className="soha-detail-stack">
      <Steps
        current={assessment.currentStep}
        size="small"
        items={[
          { title: localeCode === 'zh_CN' ? '已声明' : 'Declared', status: 'finish' },
          {
            title: localeCode === 'zh_CN' ? '已绑定' : 'Bound',
            status: claim.status === 'Bound' ? 'finish' : 'wait',
          },
          {
            title: localeCode === 'zh_CN' ? '发现 Pod 引用' : 'Pod references observed',
            status: claim.pods?.length ? 'finish' : 'wait',
          },
        ]}
      />
      {messages.map((message, index) => (
        <Alert
          key={assessment.risks[index]}
          showIcon
          type={assessment.risks[index] === 'lost' ? 'error' : 'warning'}
          title={message}
        />
      ))}
    </div>
  )
}
