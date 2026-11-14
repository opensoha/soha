import { describe, expect, it } from 'vitest'
import controllerSource from './controller.tsx?raw'
import routeStateSource from './hooks/use-workbench-route-state.ts?raw'
import modeSource from './mode.tsx?raw'
import pageSource from './page.tsx?raw'
import graphViewSource from './components/graph-view.tsx?raw'
import loaderSource from './components/graph-view-loader.tsx?raw'
import streamSource from './stream.ts?raw'

describe('AI Workbench runtime boundaries', () => {
  it('keeps the shell and route entry independent from the graph runtime', () => {
    expect(controllerSource).not.toContain("from '@xyflow/react'")
    expect(controllerSource).not.toContain("from 'dagre'")
    expect(controllerSource).not.toContain('@xyflow/react/dist/style.css')
    expect(loaderSource).toContain("lazy(() => import('./graph-view'))")
    expect(pageSource).toContain("from './controller'")
    expect(pageSource).toContain("from './mode'")
  })

  it('keeps route parsing and mode presentation outside the controller', () => {
    expect(controllerSource).toContain("from './hooks/use-workbench-route-state'")
    expect(controllerSource).toContain("from './mode'")
    expect(controllerSource).not.toContain('useSearchParams')
    expect(routeStateSource).toContain("searchParams.get('session')")
    expect(routeStateSource).toContain("searchParams.get('timeRangeMinutes')")
    expect(modeSource).toContain('export const WORKBENCH_MODE_OPTIONS')
    expect(modeSource).toContain('export function defaultAnalysisQuestion')
  })

  it('isolates React Flow and graph layout dependencies in the graph view chunk', () => {
    expect(graphViewSource).toContain("from '@xyflow/react'")
    expect(graphViewSource).toContain("from 'dagre'")
    expect(graphViewSource).toContain('@xyflow/react/dist/style.css')
  })

  it('keeps the canonical stream implementation inside the capability boundary', () => {
    expect(streamSource).toContain('export async function streamWorkbenchMessage')
    expect(streamSource).not.toContain('../workbench-stream')
  })
})
