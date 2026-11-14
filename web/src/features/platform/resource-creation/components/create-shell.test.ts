import { describe, expect, it } from 'vitest'
import { ApiError } from '@/services/api-error'
import { resourceCreationErrorMessage } from './create-shell'

describe('resource creation error message', () => {
  it('shows the backend business reason unchanged and retains the request ID', () => {
    const error = new ApiError(
      422,
      '资源创建预检未通过：Service/checkout 的端口必须在 1 到 65535 之间',
      { code: 'resource_dry_run_failed', requestId: 'request-1' },
    )

    expect(resourceCreationErrorMessage(error, true)).toBe(
      '资源创建预检未通过：Service/checkout 的端口必须在 1 到 65535 之间（请求 ID：request-1）',
    )
  })
})
