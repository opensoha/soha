/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useDeferredOverlay } from './use-deferred-overlay'

let overlay: ReturnType<typeof useDeferredOverlay<HTMLButtonElement>>
let root: ReturnType<typeof createRoot>
let host: HTMLDivElement
function Harness() {
  overlay = useDeferredOverlay<HTMLButtonElement>()
  const trigger = (
    <button ref={overlay.triggerRef} onClick={overlay.activate}>
      Trigger
    </button>
  )
  return (
    <>
      {overlay.activated ? <span data-overlay>{trigger}</span> : trigger}
      <button data-other>Other</button>
    </>
  )
}
beforeEach(async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  await act(async () => root.render(<Harness />))
})
afterEach(async () => {
  await act(async () => root.unmount())
  host.remove()
  vi.unstubAllGlobals()
})
it('restores a focused trigger after the first overlay wrapper mounts and keeps it after closing', async () => {
  const original = host.querySelector('button')!
  await act(async () => {
    original.focus()
    original.click()
  })
  const trigger = host.querySelector('button')!
  expect(trigger).not.toBe(original)
  expect(document.activeElement).toBe(trigger)
  expect(overlay.activated).toBe(true)
  expect(overlay.open).toBe(true)
  await act(async () => overlay.setOpen(false))
  expect(overlay.activated).toBe(true)
  expect(host.querySelector('button')).toBe(trigger)
  expect(document.activeElement).toBe(trigger)
})
it('does not steal focus after the user moves to another control before activation commits', async () => {
  const trigger = host.querySelector('button')!
  const other = host.querySelector<HTMLButtonElement>('[data-other]')!
  await act(async () => {
    trigger.focus()
    overlay.activate()
    other.focus()
  })
  expect(document.activeElement).toBe(other)
  expect(overlay.open).toBe(true)
})
it('does not focus a trigger activated only by hover', async () => {
  await act(async () => overlay.activate())
  expect(document.activeElement).toBe(document.body)
  expect(overlay.open).toBe(true)
})

it('cancels delayed hover on leave, explicit close, immediate activation and unmount', async () => {
  vi.useFakeTimers()
  try {
    await act(async () => overlay.activateOnHover())
    await act(async () => vi.advanceTimersByTime(50))
    expect(overlay.activated).toBe(false)
    await act(async () => overlay.cancelHover())
    await act(async () => vi.advanceTimersByTime(100))
    expect(overlay.activated).toBe(false)
    await act(async () => overlay.activateOnHover())
    await act(async () => overlay.setOpen(false))
    await act(async () => vi.advanceTimersByTime(100))
    expect(overlay.activated).toBe(false)
    await act(async () => overlay.activateOnHover())
    await act(async () => overlay.activate())
    expect(overlay.open).toBe(true)
    await act(async () => overlay.setOpen(false))
    await act(async () => vi.advanceTimersByTime(100))
    expect(overlay.open).toBe(false)
    await act(async () => root.render(<Harness key="new" />))
    await act(async () => overlay.activateOnHover())
    expect(vi.getTimerCount()).toBe(1)
    await act(async () => root.render(null))
    expect(vi.getTimerCount()).toBe(0)
  } finally {
    vi.useRealTimers()
  }
})
