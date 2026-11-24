import { useState } from 'react'
import { Button, Select, Space, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import type { EnvironmentServiceReference } from '@opensoha/contracts/gen/ts/sohaapi'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useRealtimeSessionDock } from '@/features/platform'
import { readReferenceOwnedPods, referenceTargetRoots } from './reference-session'

export function ReferencedPodSessions({ reference }: { reference: EnvironmentServiceReference }) {
  const { applicationId, serviceId, consumer, source } = reference.preview
  const { openSession } = useRealtimeSessionDock()
  const permissions = usePermissionSnapshot()
  const snapshot = permissions.isError ? undefined : permissions.data?.data
  const mayView = hasPermission(snapshot, 'platform.pods.view')
  const kinds = (['logs', 'terminal'] as const).filter((kind) =>
    hasPermission(snapshot, kind === 'logs' ? 'platform.pods.logs' : 'platform.pods.exec'),
  )
  const targets = reference.preview.targets.flatMap((target) =>
    referenceTargetRoots(target).map((root) => ({
      target,
      root,
      key: JSON.stringify([target.targetId, root.uid]),
    })),
  )
  const [selectedId, setSelectedId] = useState<string>()
  const selected =
    targets.find(({ key }) => key === selectedId) ?? (targets.length === 1 ? targets[0] : undefined)
  const relations = useQuery({
    queryKey: [
      'delivery',
      'reference-owned-pods',
      source.applicationEnvironmentId,
      reference.id,
      reference.revision,
      selected?.key,
    ],
    queryFn: ({ signal }) =>
      selected
        ? readReferenceOwnedPods(
            source.applicationEnvironmentId,
            selected.target,
            selected.root,
            signal,
          )
        : [],
    enabled: Boolean(mayView && selected),
    gcTime: 0,
    retry: false,
  })
  const pods = relations.isSuccess ? relations.data : []
  return (
    <section aria-label="引用来源 Pod" className="soha-service-runtime-overview">
      <div className="soha-service-section-heading">
        <h2>引用来源 Pod</h2>
        {targets.length > 1 ? (
          <Select
            aria-label="选择来源目标"
            placeholder="选择来源目标"
            value={selectedId}
            onChange={setSelectedId}
            options={targets.map(({ target, root, key }) => ({
              value: key,
              label: `${root.kind} / ${root.name} · ${target.namespace}`,
            }))}
          />
        ) : null}
      </div>
      <ReadFeedback query={relations} label="来源 Pod 归属" />
      {!mayView ? (
        <ManagementState compact kind="no-permission" />
      ) : !targets.length ? (
        <ManagementState compact kind="not-configured" title="请在来源环境查看此类型的 Pod" />
      ) : !selected ? (
        <ManagementState compact kind="empty" title="选择来源目标查看所属 Pod" />
      ) : relations.isPending ? (
        <ManagementState compact kind="loading" />
      ) : relations.isError ? (
        <ManagementState
          compact
          kind="error"
          title="无法核对来源 Pod 归属"
          actions={<Button onClick={() => void relations.refetch()}>重试</Button>}
        />
      ) : !pods.length ? (
        <ManagementState compact kind="empty" title="未查到已确认归属的 Pod" />
      ) : (
        <Space orientation="vertical">
          {pods.map((pod) => (
            <Space key={pod.uid} wrap>
              <Typography.Text>{pod.name}</Typography.Text>
              {kinds.map((kind) => {
                const label = kind === 'logs' ? '日志' : '终端'
                return (
                  <Button
                    key={kind}
                    size="small"
                    aria-label={`查看 ${pod.name} ${label}`}
                    onClick={() =>
                      openSession({
                        kind,
                        clusterId: pod.clusterId,
                        namespace: pod.namespace,
                        podName: pod.name,
                        podUid: pod.uid,
                        reference: {
                          applicationId,
                          serviceId,
                          viewEnvironmentId: consumer.applicationEnvironmentId,
                          sourceEnvironmentId: source.applicationEnvironmentId,
                          referenceId: reference.id,
                          referenceRevision: reference.revision,
                          targetId: selected.target.targetId,
                          rootUid: selected.root.uid,
                        },
                      })
                    }
                  >
                    {label}
                  </Button>
                )
              })}
            </Space>
          ))}
        </Space>
      )}
    </section>
  )
}
