import { createContext, useContext } from 'react'
import type { ManagedContentRecord } from '@opensoha/contracts/gen/ts/sohaapi'
export interface TransferAccess {
  view: boolean
  create: boolean
  download: boolean
  remove: boolean
  destinationView: boolean
  update: boolean
  publish: boolean
  events: boolean
  link: boolean
  review: boolean
}
interface Context {
  viewer: string
  scope: string
  access: TransferAccess
  text: (zh: string, en: string) => string
  resume: (record: ManagedContentRecord) => void
}
export const TransferContext = createContext<Context | null>(null)
export function useTransferContext() {
  const value = useContext(TransferContext)
  if (!value) throw new Error('Missing managed content context')
  return value
}
