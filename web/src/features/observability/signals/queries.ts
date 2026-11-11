import type {
  ObservabilityMetricQueryInput,
  ObservabilityTraceQueryInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { queryOptions } from '@tanstack/react-query'
import { observabilityKeys } from '../keys'
import {
  getService,
  getServiceTopology,
  listMetricCatalog,
  listServices,
  queryMetrics,
  queryTraces,
  type ObservabilityServiceQuery,
} from './api'

export const observabilitySignalQueries = {
  metrics: (input?: ObservabilityMetricQueryInput) =>
    queryOptions({
      queryKey: observabilityKeys.signals.metrics(input),
      queryFn: () => queryMetrics(input!),
      enabled: Boolean(input),
      retry: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    }),
  traces: (input?: ObservabilityTraceQueryInput) =>
    queryOptions({
      queryKey: observabilityKeys.signals.traces(input),
      queryFn: () => queryTraces(input!),
      enabled: Boolean(input),
      retry: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    }),
  metricCatalog: () =>
    queryOptions({
      queryKey: observabilityKeys.signals.metricCatalog(),
      queryFn: listMetricCatalog,
      staleTime: 30_000,
    }),
  services: (query?: ObservabilityServiceQuery) =>
    queryOptions({
      queryKey: observabilityKeys.signals.services(query),
      queryFn: () => listServices(query!),
      enabled: Boolean(query),
    }),
  service: (serviceId: string, query?: ObservabilityServiceQuery) =>
    queryOptions({
      queryKey: observabilityKeys.signals.service(serviceId, query),
      queryFn: () => getService(serviceId, query!),
      enabled: Boolean(serviceId && query),
    }),
  topology: (serviceId: string, query?: ObservabilityServiceQuery) =>
    queryOptions({
      queryKey: observabilityKeys.signals.topology(serviceId, query),
      queryFn: () => getServiceTopology(serviceId, query!),
      enabled: Boolean(serviceId && query),
    }),
}
