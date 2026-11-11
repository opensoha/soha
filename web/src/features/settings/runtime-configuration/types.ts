export type {
  RuntimeConfigApplication,
  RuntimeConfigApplicationStatus,
  RuntimeConfigApplyMode,
  RuntimeConfigApplyResult,
  RuntimeConfigChange,
  RuntimeConfigChangeRequest,
  RuntimeConfigItem,
  RuntimeConfigRevision,
  RuntimeConfigRollbackRequest,
  RuntimeConfigSnapshot,
  RuntimeConfigSource,
  RuntimeConfigValidatedChange,
  RuntimeConfigValidationIssue,
  RuntimeConfigValidationResult,
  RuntimeConfigValue,
  RuntimeConfigValueType,
  RuntimeResourceSnapshot,
} from '@opensoha/contracts/gen/ts/sohaapi'

export interface RuntimeConfigFilters {
  keyword?: string
  applyMode?: string
  source?: string
}
