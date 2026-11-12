import { api } from '@/services/api-client'
import type {
  NetworkRADIUSOptions,
  NetworkRADIUSPool,
  NetworkRADIUSPoolList,
  NetworkRADIUSPoolInput,
  NetworkRADIUSConfigurationInput,
  NetworkRADIUSControlInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
const base = '/network-access/radius-pools'
const path = (id: string) => `${base}/${encodeURIComponent(id)}`
const unwrap = async <T>(p: Promise<{ data: T }>) => (await p).data
export const radiusApi = {
  options: () => unwrap(api.get<{ data: NetworkRADIUSOptions }>(`${base}/options`)),
  list: () => unwrap(api.get<{ data: NetworkRADIUSPoolList }>(base)),
  create: (input: NetworkRADIUSPoolInput) =>
    unwrap(api.post<{ data: NetworkRADIUSPool }>(base, input)),
  configure: (id: string, input: NetworkRADIUSConfigurationInput) =>
    unwrap(api.put<{ data: NetworkRADIUSPool }>(`${path(id)}/configuration`, input)),
  control: (id: string, input: NetworkRADIUSControlInput) =>
    unwrap(api.post<{ data: NetworkRADIUSPool }>(`${path(id)}/control`, input)),
}
