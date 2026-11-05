import { useQueryClient } from '@tanstack/react-query'
import { App } from 'antd'
import { useCallback, useContext, useLayoutEffect, useRef } from 'react'
import {
  UNSAFE_DataRouterContext,
  UNSAFE_NavigationContext,
  useBeforeUnload,
  useLocation,
} from 'react-router-dom'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { registerBrowserPopGuard } from '@/utils/browser-pop-guard'
import { crdKeys } from './keys'

export type CRDEditorState = { dirty: boolean; busy: boolean; discard?: () => void }

/** Local to the CRD workbench; BrowserRouter does not provide data-router blockers. */
export function useCRDNavigationGuard() {
  const { modal } = App.useApp()
  const { localeCode } = useI18n()
  const { navigator } = useContext(UNSAFE_NavigationContext)
  const dataRouter = useContext(UNSAFE_DataRouterContext)?.router
  const location = useLocation()
  const client = useQueryClient()
  const state = useRef<CRDEditorState>({ dirty: false, busy: false })
  const bypass = useRef(false)
  const confirmation = useRef<{ destroy: () => void } | null>(null)
  const browserLocation = useRef({ href: window.location.href, state: window.history.state })
  const allowPop = useRef(false)

  const setEditorState = useCallback((next: CRDEditorState) => {
    state.current = next
  }, [])
  const writeBlocked = useCallback(() => {
    const clusterId = usePlatformScopeStore.getState().clusterId
    return (
      state.current.busy ||
      client.isMutating({
        mutationKey: crdKeys.apply(),
        exact: true,
        predicate: ({ state: { variables } }) =>
          typeof variables === 'object' &&
          variables !== null &&
          'clusterId' in variables &&
          variables.clusterId === clusterId,
      }) > 0
    )
  }, [client])

  const requestLeave = useCallback(
    (action: () => void) => {
      if (bypass.current) return action()
      if (writeBlocked() || confirmation.current) return
      const proceed = () => {
        if (writeBlocked()) return
        state.current.discard?.()
        state.current = { dirty: false, busy: false }
        bypass.current = true
        try {
          action()
        } finally {
          bypass.current = false
        }
      }
      if (!state.current.dirty) return proceed()
      const zh = localeCode === 'zh_CN'
      confirmation.current = modal.confirm({
        title: zh ? '离开当前资源？' : 'Leave this resource?',
        content: zh
          ? '尚未应用的更改会被放弃。已保存的本地草稿仍会保留。'
          : 'Unapplied changes will be discarded. Saved local drafts are retained.',
        okText: zh ? '放弃更改并切换' : 'Discard and switch',
        cancelText: zh ? '取消' : 'Cancel',
        onCancel: () => {
          confirmation.current = null
        },
        onOk: () => {
          confirmation.current = null
          proceed()
        },
      })
    },
    [localeCode, modal, writeBlocked],
  )

  useBeforeUnload((event) => {
    if (state.current.dirty || writeBlocked()) {
      event.preventDefault()
      event.returnValue = ''
    }
  })

  useLayoutEffect(() => {
    browserLocation.current = { href: window.location.href, state: window.history.state }
  }, [location])

  useLayoutEffect(() => {
    const original = { push: navigator.push, replace: navigator.replace, go: navigator.go }
    const browserHistory = !dataRouter && !('index' in navigator)
    const push: typeof navigator.push = (...args) => requestLeave(() => original.push(...args))
    const replace: typeof navigator.replace = (...args) =>
      requestLeave(() => original.replace(...args))
    const go: typeof navigator.go = (...args) => requestLeave(() => original.go(...args))
    navigator.push = push
    navigator.replace = replace
    navigator.go = go

    // Data routers can navigate without their Navigator (e.g. router.navigate(-1)).
    const originalNavigate = dataRouter?.navigate
    const navigate =
      originalNavigate &&
      ((
        to: number | Parameters<typeof originalNavigate>[0],
        options?: Parameters<typeof originalNavigate>[1],
      ) => {
        let result = Promise.resolve()
        requestLeave(() => {
          result = typeof to === 'number' ? originalNavigate(to) : originalNavigate(to, options)
        })
        return result
      })
    if (dataRouter && navigate) dataRouter.navigate = navigate

    let restoring: { index: number; delta: number } | null = null
    const onPop = (event: PopStateEvent) => {
      if (!browserHistory) return
      const nextIndex: unknown = window.history.state?.idx
      if (restoring) {
        event.stopImmediatePropagation()
        if (nextIndex !== restoring.index && typeof nextIndex === 'number') {
          window.history.go(restoring.index - nextIndex)
          return
        }
        const { delta } = restoring
        restoring = null
        requestLeave(() => {
          allowPop.current = true
          window.history.go(delta)
        })
        return
      }
      if (allowPop.current) {
        allowPop.current = false
        return
      }
      if (!state.current.dirty && !writeBlocked()) return
      event.stopImmediatePropagation()
      const previous = browserLocation.current
      const previousIndex: unknown = previous.state?.idx
      if (
        typeof nextIndex === 'number' &&
        typeof previousIndex === 'number' &&
        nextIndex !== previousIndex
      ) {
        restoring = { index: previousIndex, delta: nextIndex - previousIndex }
        window.history.go(previousIndex - nextIndex)
      } else {
        // An entry written outside Router has no index; retain the live editor and its URL.
        window.history.replaceState(previous.state, '', previous.href)
      }
    }
    const unregister = browserHistory ? registerBrowserPopGuard(onPop) : undefined
    return () => {
      unregister?.()
      if (navigator.push === push) navigator.push = original.push
      if (navigator.replace === replace) navigator.replace = original.replace
      if (navigator.go === go) navigator.go = original.go
      if (dataRouter && dataRouter.navigate === navigate && originalNavigate)
        dataRouter.navigate = originalNavigate
      confirmation.current?.destroy()
      confirmation.current = null
    }
  }, [dataRouter, navigator, requestLeave, writeBlocked])

  return { requestLeave, setEditorState }
}
