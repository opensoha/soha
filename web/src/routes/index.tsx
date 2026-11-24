import { lazy, Suspense, useEffect } from 'react'
import { Route, Routes, useMatch, useLocation } from 'react-router-dom'
import { Spin } from 'antd'
import { AuthGuard } from '@/features/auth/auth-guard'
import { useAuthStore } from '@/stores/auth-store'
import { getRouteMeta } from './meta'
import { registeredRouteDefinitions } from './registry'
import { renderRegisteredRoutes } from './router'

const loadAppLayout = () =>
  import('@/layouts/app-layout').then((module) => ({ default: module.AppLayout }))
const AppLayout = lazy(loadAppLayout)
const loadApplicationEntryReads = () =>
  import('@/features/delivery/applications/entry-reads')
    .then((module) => ({ default: module.ApplicationEntryReads }))
    .catch(() => ({ default: () => null }))
const ApplicationEntryReads = lazy(loadApplicationEntryReads)
const applicationDetail = registeredRouteDefinitions.find(
  (definition) => definition.meta.id === 'application-detail',
)

function ApplicationRouteLayout() {
  const { pathname } = useLocation()
  const applicationMatch = useMatch('/applications/:applicationId')
  const applicationId =
    getRouteMeta(pathname).id === 'application-detail'
      ? applicationMatch?.params.applicationId?.trim()
      : ''
  return (
    <>
      {applicationId ? (
        <Suspense fallback={null}>
          <ApplicationEntryReads applicationId={applicationId} />
        </Suspense>
      ) : null}
      <Suspense fallback={<Spin size="large" fullscreen />}>
        <AppLayout />
      </Suspense>
    </>
  )
}

export function AppRouter() {
  const { pathname } = useLocation()
  const applicationMatch = useMatch('/applications/:applicationId')
  const hasSession = useAuthStore((state) => Boolean(state.user))
  useEffect(() => {
    if (!hasSession || !applicationMatch || getRouteMeta(pathname).id !== 'application-detail')
      return
    // These imports only warm static modules. AuthGuard still owns session and permission checks.
    void loadApplicationEntryReads()
    void loadAppLayout().catch(() => {})
    void applicationDetail?.load?.()?.catch(() => {})
  }, [applicationMatch, hasSession, pathname])

  return (
    <Routes>
      {renderRegisteredRoutes('public')}
      <Route element={<AuthGuard />}>
        {renderRegisteredRoutes('portal')}
        {renderRegisteredRoutes('focus')}
        <Route element={<ApplicationRouteLayout />}>{renderRegisteredRoutes('app')}</Route>
      </Route>
    </Routes>
  )
}
