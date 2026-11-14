import { lazy, Suspense } from 'react'

import type { WorkbenchGraph } from '../types'

const WorkbenchGraphView = lazy(() => import('./graph-view'))

export function LazyWorkbenchGraphView({
  fitKey,
  graph,
  selectedNodeId,
  onSelectNode,
}: {
  fitKey: string
  graph: WorkbenchGraph
  selectedNodeId: string | null
  onSelectNode: (nodeId: string | null) => void
}) {
  return (
    <Suspense fallback={<div className="soha-workbench-graph-canvas" aria-busy="true" />}>
      <WorkbenchGraphView
        fitKey={fitKey}
        graph={graph}
        selectedNodeId={selectedNodeId}
        onSelectNode={onSelectNode}
      />
    </Suspense>
  )
}
