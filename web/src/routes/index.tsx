import { lazy, Suspense } from 'react'
import { Route, Routes } from 'react-router-dom'
import { Spin } from 'antd'
import { AuthGuard } from '@/features/auth/auth-guard'
import { renderRegisteredRoutes } from './router'

const AppLayout = lazy(() =>
  import('@/layouts/app-layout').then((module) => ({ default: module.AppLayout })),
)

export function AppRouter() {
  return (
    <Routes>
      {renderRegisteredRoutes('public')}
      <Route element={<AuthGuard />}>
        {renderRegisteredRoutes('portal')}
        {renderRegisteredRoutes('focus')}
        <Route
          element={
            <Suspense fallback={<Spin size="large" fullscreen />}>
              <AppLayout />
            </Suspense>
          }
        >
          {renderRegisteredRoutes('app')}
        </Route>
      </Route>
    </Routes>
  )
}
