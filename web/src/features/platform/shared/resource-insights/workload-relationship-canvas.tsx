import { ReactFlowProvider, type OnMove, type Viewport } from '@xyflow/react'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { useMemo } from 'react'
import { useI18n } from '@/i18n'
import { ResourceGraphCanvas, buildResourceGraphFlow } from './resource-graph-panel'
import { isUnresolvedStorageBinding, relationshipLabel } from './relationship-model'

export default function WorkloadRelationshipCanvas({
  graph,
  selectedId,
  onSelect,
  defaultViewport,
  onMove,
}: {
  graph: KubernetesResourceGraph
  selectedId: string | null
  onSelect: (id: string | null) => void
  defaultViewport?: Viewport
  onMove: OnMove
}) {
  const { localeCode } = useI18n()
  const flow = useMemo(() => {
    const result = buildResourceGraphFlow({
      ...graph,
      nodes: graph.nodes.map((node) =>
        node.status === 'unresolved' ? { ...node, health: undefined } : node,
      ),
    })
    const edgeById = new Map(graph.edges.map((edge) => [edge.id, edge]))
    return {
      ...result,
      edges: result.edges.map((edge) => {
        const token = String(edge.label)
        const source = edgeById.get(edge.id)
        const resolution = source?.resolutionState
        return {
          ...edge,
          label: relationshipLabel(token, localeCode, true, resolution, source?.fieldPath),
          className: isUnresolvedStorageBinding(token, resolution)
            ? 'soha-unresolved-storage-binding'
            : edge.className,
        }
      }),
    }
  }, [graph, localeCode])
  return (
    <ReactFlowProvider>
      <ResourceGraphCanvas
        graph={graph}
        flow={flow}
        selectedId={selectedId}
        onSelect={onSelect}
        defaultViewport={defaultViewport}
        onMove={onMove}
        hideInspector
        fitViewKey={graph.rootId}
      />
    </ReactFlowProvider>
  )
}
