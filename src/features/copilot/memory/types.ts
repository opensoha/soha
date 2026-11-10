import type {
  AIMemoryPolicy,
  AIMemoryPolicyInput,
  AIMemoryRecord,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { ProductionRecord } from '../production/operations-page'

export type MemoryRecord = AIMemoryRecord & ProductionRecord
export type MemoryPolicy = AIMemoryPolicy & ProductionRecord
export type CreateMemoryPolicyInput = AIMemoryPolicyInput
