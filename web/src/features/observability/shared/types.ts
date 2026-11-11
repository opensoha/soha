export type ObservabilityJsonPrimitive = string | number | boolean | null

export type ObservabilityJsonValue =
  | ObservabilityJsonPrimitive
  | ObservabilityPayloadMap
  | ObservabilityJsonValue[]

export interface ObservabilityPayloadMap {
  [key: string]: ObservabilityJsonValue
}
