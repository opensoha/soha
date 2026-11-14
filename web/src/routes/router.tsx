import { lazy, Suspense, type ComponentType } from 'react'
import { Navigate, Route, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import { getRegisteredRoutesByShell } from './registry'
import type { AppRouteShell, ResolvedAppRouteDefinition } from './route-types'

const lazyComponents = new Map<string, ComponentType>()

function RouteRedirect({ to }: { to: string }) {
  const { search, hash } = useLocation()
  return <Navigate to={{ pathname: to, search, hash }} replace />
}

function getLazyRouteComponent(definition: ResolvedAppRouteDefinition) {
  const cached = lazyComponents.get(definition.meta.id)
  if (cached) return cached
  if (!definition.load) return undefined
  const component = lazy(definition.load)
  lazyComponents.set(definition.meta.id, component)
  return component
}

function renderRouteElement(definition: ResolvedAppRouteDefinition) {
  if (definition.redirectTo) return <RouteRedirect to={definition.redirectTo} />
  const Component = getLazyRouteComponent(definition)
  if (!Component) return null
  return (
    <Suspense
      fallback={
        <div className="flex min-h-[240px] items-center justify-center">
          <Spin size="large" />
        </div>
      }
    >
      <Component />
    </Suspense>
  )
}

export function renderRegisteredRoutes(shell: AppRouteShell) {
  return getRegisteredRoutesByShell(shell).flatMap((definition) => [
    <Route
      key={definition.meta.id}
      path={definition.meta.path}
      element={renderRouteElement(definition)}
    />,
    ...(definition.aliases ?? []).map((alias) => (
      <Route
        key={`${definition.meta.id}:${alias}`}
        path={alias}
        element={renderRouteElement(definition)}
      />
    )),
  ])
}
