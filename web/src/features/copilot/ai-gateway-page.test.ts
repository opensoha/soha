import { describe, expect, it } from 'vitest'
import {
  accessPolicyApprovalPolicyFromValues,
  accessPolicyConditionsFromValues,
  accessPolicyFormValuesFromRecord,
  governanceApprovalQueueRows,
  governanceCoverageRows,
  governanceCoverageDrilldown,
  governanceFindingDrilldownActions,
  governancePolicyDraftForCoverage,
  governanceQueueDrilldown,
  governanceRecommendationDrilldownAction,
  governanceRedactionRows,
  governanceRiskCountTags,
  governanceTokenFindingDrilldown,
  governanceTokenFindingRows,
  gatewaySecretTypeOptions,
  gatewayTimeRangeQuery,
  rateLimitModeOptions,
} from './ai-gateway-page'

describe('AI Gateway policy condition helpers', () => {
  it('builds backend-supported approval routing from Console policy fields', () => {
    expect(accessPolicyApprovalPolicyFromValues({
      approvalMode: 'require_approval',
      approvalPolicyRef: 'delivery-standard',
      approvalRoutingMode: 'all',
      approvalApproverUsers: [' release-owner ', 'qa-owner'],
      approvalApproverRoles: ['release-manager'],
      approvalApproverTeams: ['security'],
      approvalOnCallRef: 'sre-primary',
      approvalRequiredApprovals: 2,
      approvalChangeWindowStartsAt: '2026-06-01T09:00:00Z',
      approvalChangeWindowEndsAt: '2026-06-01T18:00:00Z',
      approvalChangeWindowTimezone: 'Asia/Shanghai',
    })).toEqual({
      strategy: 'require_approval',
      approvalPolicyRef: 'delivery-standard',
      approvalMode: 'all',
      approverUsers: ['release-owner', 'qa-owner'],
      approverRoles: ['release-manager'],
      approverTeams: ['security'],
      onCallRef: 'sre-primary',
      requiredApprovals: 2,
      changeWindow: {
        startsAt: '2026-06-01T09:00:00Z',
        endsAt: '2026-06-01T18:00:00Z',
        timezone: 'Asia/Shanghai',
      },
    })

    expect(accessPolicyApprovalPolicyFromValues({ approvalMode: 'none', approvalPolicyRef: 'ignored' })).toEqual({})
  })

  it('builds backend-supported conditions from Console policy fields', () => {
    expect(accessPolicyConditionsFromValues({
      rateLimitEnabled: true,
      rateLimitMode: 'gcra',
      rateLimitScope: 'actor_client_tool',
      rateLimitMaxCallsPerMinute: 60,
      rateLimitBurst: 20,
      budgetEnabled: true,
      budgetScope: 'actor_client',
      budgetMaxCallsPerDay: 500,
      budgetMaxTokensPerDay: 200000,
      budgetMaxCostPerDay: 15,
      redactionEnabled: true,
      redactionMode: 'sanitize',
      redactionTarget: 'both',
      redactionFields: ['metadata.apiToken', ' buildSources.*.config.password '],
      redactionAllowFields: ['search'],
      redactionSecretTypes: ['openai', 'github'],
      redactionValuePatterns: ['APP-[0-9]{4}'],
      redactionReplacement: '[MASKED]',
      redactionPreserveFormat: true,
      outputRedactionFields: ['application.buildSources.*.config.token'],
      outputRedactionSecretTypes: ['openrouter'],
      outputRedactionValuePatterns: ['token=[A-Za-z0-9_-]{16,}'],
      outputRedactionReplacement: '[OUTPUT]',
      outputRedactionPreserveFormat: true,
    })).toEqual({
      rateLimit: {
        mode: 'gcra',
        scope: 'actor_client_tool',
        maxCallsPerMinute: 60,
        burst: 20,
      },
      budget: {
        scope: 'actor_client',
        maxCallsPerDay: 500,
        maxTokensPerDay: 200000,
        maxCostPerDay: 15,
      },
      redactionPolicy: {
        mode: 'sanitize',
        target: 'both',
        fields: ['metadata.apiToken', 'buildSources.*.config.password'],
        allowFields: ['search'],
        secretTypes: ['openai', 'github'],
        valuePatterns: ['APP-[0-9]{4}'],
        replacement: '[MASKED]',
        preserveFormat: true,
      },
      outputRedactionPolicy: {
        mode: 'sanitize',
        fields: ['application.buildSources.*.config.token'],
        secretTypes: ['openrouter'],
        valuePatterns: ['token=[A-Za-z0-9_-]{16,}'],
        replacement: '[OUTPUT]',
        preserveFormat: true,
      },
    })
  })

  it('normalizes persisted conditions back into Console form values', () => {
    expect(accessPolicyFormValuesFromRecord({
      id: 'policy-1',
      name: 'Delivery guardrail',
      enabled: true,
      subjectType: 'role',
      subjectId: 'developer',
      effect: 'allow',
      approvalPolicy: {
        strategy: 'require_approval',
        approvalPolicyRef: 'delivery-standard',
        approvalRouting: {
          approvalMode: 'any',
          candidateUserIds: ['release-owner'],
          candidateRoles: ['release-manager'],
          candidateTeams: ['security'],
          onCallRef: 'sre-primary',
          requiredApprovals: 2,
          changeWindow: {
            startsAt: '2026-06-01T09:00:00Z',
            endsAt: '2026-06-01T18:00:00Z',
            timezone: 'Asia/Shanghai',
          },
        },
      },
      riskLevels: ['execute'],
      conditions: {
        rateLimit: { mode: 'token_bucket', scope: 'actor_client', maxCallsPerHour: 10, burst: 3 },
        budget: { maxTokensPerDay: 10000, maxCostPerDay: 2.5 },
        redactionPolicy: {
          mode: 'mask',
          target: 'input',
          fields: ['metadata.token'],
          secretTypes: ['openai'],
          preserveFormat: true,
        },
        outputRedactionPolicy: {
          fields: ['result.secret'],
          secretTypes: ['openrouter'],
          valuePatterns: ['token=[A-Za-z0-9_-]{16,}'],
          replacement: '[OUTPUT]',
          preserveFormat: true,
        },
      },
      createdAt: '2026-05-29T00:00:00Z',
      updatedAt: '2026-05-29T00:00:00Z',
    })).toMatchObject({
      approvalMode: 'require_approval',
      approvalPolicyRef: 'delivery-standard',
      approvalRoutingMode: 'any',
      approvalApproverUsers: ['release-owner'],
      approvalApproverRoles: ['release-manager'],
      approvalApproverTeams: ['security'],
      approvalOnCallRef: 'sre-primary',
      approvalRequiredApprovals: 2,
      approvalChangeWindowStartsAt: '2026-06-01T09:00:00Z',
      approvalChangeWindowEndsAt: '2026-06-01T18:00:00Z',
      approvalChangeWindowTimezone: 'Asia/Shanghai',
      rateLimitEnabled: true,
      rateLimitMode: 'gcra',
      rateLimitScope: 'actor_client',
      rateLimitMaxCallsPerHour: 10,
      rateLimitBurst: 3,
      budgetEnabled: true,
      budgetMaxTokensPerDay: 10000,
      budgetMaxCostPerDay: 2.5,
      redactionEnabled: true,
      redactionMode: 'mask',
      redactionTarget: 'input',
      redactionFields: ['metadata.token'],
      redactionSecretTypes: ['openai'],
      redactionPreserveFormat: true,
      outputRedactionFields: ['result.secret'],
      outputRedactionSecretTypes: ['openrouter'],
      outputRedactionValuePatterns: ['token=[A-Za-z0-9_-]{16,}'],
      outputRedactionReplacement: '[OUTPUT]',
      outputRedactionPreserveFormat: true,
    })
  })

  it('keeps sliding-window rate limits and agent tooling secret classifiers available', () => {
    expect(rateLimitModeOptions.map((item) => item.value)).toContain('sliding_window')
    expect(gatewaySecretTypeOptions.map((item) => item.value)).toEqual(expect.arrayContaining([
      'brave_search',
      'serpapi',
      'browserbase',
      'exa',
      'jina',
      'unstructured',
      'llama_cloud',
      'helicone',
      'dashscope',
      'moonshot',
      'zhipu',
      'siliconflow',
      'hunyuan',
      'qianfan',
      'volcengine',
      'grafana',
      'sentry',
      'newrelic',
      'azure_openai',
      'azure_devops',
      'datadog',
      'pagerduty',
      'posthog',
      'splunk',
      'elastic',
      'terraform',
    ]))
    expect(accessPolicyConditionsFromValues({
      rateLimitEnabled: true,
      rateLimitMode: 'sliding_window',
      rateLimitScope: 'actor_client',
      rateLimitMaxCallsPerHour: 25,
      redactionEnabled: true,
      redactionMode: 'strict',
      redactionTarget: 'both',
      redactionSecretTypes: ['grafana', 'sentry', 'newrelic', 'azure_openai', 'azure_devops', 'datadog', 'pagerduty', 'posthog', 'splunk', 'elastic', 'terraform'],
    })).toMatchObject({
      rateLimit: {
        mode: 'sliding_window',
        scope: 'actor_client',
        maxCallsPerHour: 25,
      },
      redactionPolicy: {
        mode: 'strict',
        target: 'both',
        secretTypes: ['grafana', 'sentry', 'newrelic', 'azure_openai', 'azure_devops', 'datadog', 'pagerduty', 'posthog', 'splunk', 'elastic', 'terraform'],
      },
    })
    expect(accessPolicyFormValuesFromRecord({
      id: 'policy-2',
      name: 'Observability guardrail',
      enabled: true,
      subjectType: 'user',
      subjectId: 'sre',
      effect: 'allow',
      conditions: {
        rateLimit: { mode: 'rolling_window', scope: 'actor_client', maxCallsPerMinute: 5 },
        redactionPolicy: { secretTypes: ['grafana', 'sentry', 'newrelic', 'azure_openai', 'azure_devops', 'datadog', 'pagerduty', 'posthog', 'splunk', 'elastic', 'terraform'] },
      },
      createdAt: '2026-05-29T00:00:00Z',
      updatedAt: '2026-05-29T00:00:00Z',
    })).toMatchObject({
      rateLimitMode: 'sliding_window',
      redactionSecretTypes: ['grafana', 'sentry', 'newrelic', 'azure_openai', 'azure_devops', 'datadog', 'pagerduty', 'posthog', 'splunk', 'elastic', 'terraform'],
    })
  })

  it('omits disabled condition groups before submit', () => {
    expect(accessPolicyConditionsFromValues({
      rateLimitEnabled: false,
      budgetEnabled: false,
      redactionEnabled: false,
    })).toEqual({})
  })

  it('does not submit output redaction policy without output match selectors', () => {
    expect(accessPolicyConditionsFromValues({
      redactionEnabled: true,
      redactionMode: 'sanitize',
      redactionTarget: 'input',
      outputRedactionReplacement: '[OUTPUT]',
      outputRedactionPreserveFormat: true,
    })).toEqual({
      redactionPolicy: {
        mode: 'sanitize',
        target: 'input',
        replacement: '[REDACTED]',
      },
    })
  })

  it('serializes Console time range filters for Gateway list APIs', () => {
    const from = { toISOString: () => '2026-05-29T01:02:03.000Z' }
    const to = { toISOString: () => '2026-05-29T04:05:06.000Z' }

    expect(gatewayTimeRangeQuery([from, to])).toEqual({
      from: '2026-05-29T01:02:03.000Z',
      to: '2026-05-29T04:05:06.000Z',
    })
    expect(gatewayTimeRangeQuery(null)).toEqual({ from: '', to: '' })
  })

  it('summarizes governance policy coverage rows for the Console status tab', () => {
    expect(governanceCoverageRows({
      accessPolicies: 4,
      toolGrants: 3,
      skillBindings: 2,
      activeAccessPolicies: 3,
      activeToolGrants: 2,
      activeSkillBindings: 1,
      budgetPolicies: 1,
      rateLimitPolicies: 2,
      redactionPolicies: 1,
      resourceScopedAccessPolicies: 2,
      resourceScopedToolGrants: 1,
      budgetState: 'configured',
      rateLimitState: 'configured',
      redactionPolicyState: 'configured',
      resourceScopeState: 'configured',
    })).toEqual(expect.arrayContaining([
      expect.objectContaining({ key: 'budget', configured: 1, total: 3, state: 'configured' }),
      expect.objectContaining({ key: 'resource_scopes', configured: 3, total: 5, state: 'configured' }),
    ]))
  })

  it('builds access policy drafts from governance coverage gaps', () => {
    const budgetRow = governanceCoverageRows({
      accessPolicies: 0,
      activeAccessPolicies: 0,
      budgetState: 'not_configured',
    }).find((item) => item.key === 'budget')
    expect(budgetRow).toBeTruthy()
    expect(governancePolicyDraftForCoverage(budgetRow!)).toMatchObject({
      name: 'Gateway daily budget guardrail',
      subjectType: 'role',
      subjectId: 'developer',
      effect: 'allow',
      approvalMode: 'require_approval',
      budgetEnabled: true,
      rateLimitEnabled: false,
      redactionEnabled: false,
    })
    expect(governanceCoverageDrilldown(budgetRow!)).toMatchObject({
      tab: 'policies',
      policyDraft: {
        name: 'Gateway daily budget guardrail',
        budgetEnabled: true,
      },
    })

    const grantRow = governanceCoverageRows({ toolGrants: 1, activeToolGrants: 1 }).find((item) => item.key === 'tool_grants')
    expect(governanceCoverageDrilldown(grantRow!)).toEqual({ tab: 'grants' })
  })

  it('maps structured governance recommendation actions to Console drilldowns', () => {
    expect(governanceRecommendationDrilldownAction({
      type: 'approval_sla',
      severity: 'warning',
      summary: 'approval needs attention',
      action: 'resolve_gateway_approvals',
      targetKind: 'approval_requests',
      refs: ['approval-due'],
      count: 1,
    })).toEqual({
      label: '处理审批',
      target: {
        tab: 'approvals',
        approvalFilters: {
          id: 'approval-due',
          status: '',
          actor: '',
          aiClientId: '',
          toolName: '',
          riskLevel: '',
          strategy: '',
          from: '',
          to: '',
        },
      },
    })
    expect(governanceRecommendationDrilldownAction({
      type: 'high_risk_guardrails',
      severity: 'warning',
      summary: 'guard high-risk tools',
      action: 'create_high_risk_approval_guardrail',
      targetKind: 'access_policies',
      metadata: { policyTemplate: 'approval_guardrail' },
    })).toMatchObject({
      label: '创建 policy',
      target: {
        tab: 'policies',
        policyDraft: {
          name: 'Gateway governance guardrail',
          approvalMode: 'require_approval',
        },
      },
    })
    expect(governanceRecommendationDrilldownAction({
      type: 'token_hygiene',
      severity: 'warning',
      summary: 'review tokens',
      action: 'review_and_revoke_unused_tokens',
      targetKind: 'tokens',
      refs: ['pat-stale'],
    })).toEqual({
      label: '处理 token',
      target: { tab: 'tokens', tokenFilter: 'pat-stale' },
    })
    expect(governanceRecommendationDrilldownAction({
      type: 'token_hygiene',
      severity: 'warning',
      summary: 'review service tokens',
      action: 'review_and_revoke_unused_tokens',
      targetKind: 'tokens',
      refs: ['2f8e9a9b-5c7f-45b2-a8f1-7dce47288f10'],
      metadata: {
        tokenRefs: [{
          kind: 'service_account_token',
          id: '2f8e9a9b-5c7f-45b2-a8f1-7dce47288f10',
          tokenPrefix: 'soha_sat_stale',
        }],
      },
    })).toEqual({
      label: '处理 token',
      target: { tab: 'service-accounts', serviceTokenFilter: '2f8e9a9b-5c7f-45b2-a8f1-7dce47288f10' },
    })
  })

  it('summarizes redaction hits and drilldowns for the Console governance tab', () => {
    const rows = governanceRedactionRows({
      totalMatches: 5,
      auditsWithRedaction: 2,
      inputAudits: 1,
      outputAudits: 1,
      fieldMatches: 1,
      sensitiveKeyMatches: 1,
      sensitiveTextMatches: 0,
      valuePatternMatches: 1,
      secretClassifierMatches: 1,
      structuredSecretMatches: 1,
      topTargets: [{ key: 'input', count: 1 }, { key: 'output', count: 1 }],
      topMatchTypes: [{ key: 'secret_classifier', count: 1 }],
      topClassifiers: [{ key: 'openai', count: 1 }],
      topFieldPaths: [{ key: 'metadata.apiToken', count: 1 }],
      topPolicies: [{ key: 'policy-redaction', count: 2 }],
      topTools: [{ key: 'k8s.pods.logs', count: 1 }],
    })

    expect(rows).toEqual(expect.arrayContaining([
      expect.objectContaining({ key: 'targets', count: 2 }),
      expect.objectContaining({ key: 'classifiers', count: 1, items: [{ key: 'openai', count: 1 }] }),
      expect.objectContaining({ key: 'policies', target: { tab: 'policies', policyFilter: 'policy-redaction' } }),
      expect.objectContaining({ key: 'tools', target: { tab: 'audit', auditFilters: expect.objectContaining({ toolName: 'k8s.pods.logs' }) } }),
    ]))
  })

  it('maps governance risk counts and approval queues for the Console status tab', () => {
    expect(governanceRiskCountTags({ execute: 2, read: 8, mutate: 2, ignored: 0 })).toEqual([
      'read:8',
      'execute:2',
      'mutate:2',
    ])

    const queueRows = governanceApprovalQueueRows({
      dueSoon: 1,
      stalePending: 2,
      overdue: 1,
      dueSoonRequestIds: ['approval-due'],
      stalePendingRequestIds: ['approval-stale-1', 'approval-stale-2'],
      overdueRequestIds: ['approval-overdue'],
    }, {
      pendingApproval: 2,
      pendingApprovalClientIds: ['codex-local', 'ci-agent'],
    })
    expect(queueRows).toEqual([
      { key: 'due_soon', label: 'Due soon approvals', count: 1, refs: ['approval-due'] },
      { key: 'stale', label: 'Stale approvals', count: 2, refs: ['approval-stale-1', 'approval-stale-2'] },
      { key: 'overdue', label: 'Overdue approvals', count: 1, refs: ['approval-overdue'] },
      { key: 'pending_clients', label: 'Pending AI clients', count: 2, refs: ['codex-local', 'ci-agent'] },
    ])
    expect(governanceQueueDrilldown(queueRows[0], ' approval-due ')).toEqual({
      tab: 'approvals',
      approvalFilters: {
        id: 'approval-due',
        status: '',
        actor: '',
        aiClientId: '',
        toolName: '',
        riskLevel: '',
        strategy: '',
        from: '',
        to: '',
      },
    })
    expect(governanceQueueDrilldown(queueRows[3], ' codex-local ')).toEqual({
      tab: 'clients',
      clientFilter: 'codex-local',
    })
  })

  it('builds governance finding drilldown actions for Console triage', () => {
    const actions = governanceFindingDrilldownActions({
      type: 'approval_sla_due_soon',
      severity: 'warning',
      summary: 'approval needs attention',
      count: 1,
      actorType: 'user',
      actorId: 'user-1',
      aiClientId: 'codex-local',
      policyId: 'policy-risk-open',
      grantId: 'grant-risk-open',
      approvalRequestId: 'approval-due',
      toolName: 'delivery.actions.trigger',
      riskLevel: 'execute',
    })

    expect(actions.map((item) => item.label)).toEqual(['查看审批', '查看 client', '查看 policy', '查看 grant', '查日志'])
    expect(actions[0].target).toEqual({
      tab: 'approvals',
      approvalFilters: {
        id: 'approval-due',
        status: '',
        actor: '',
        aiClientId: '',
        toolName: '',
        riskLevel: '',
        strategy: '',
        from: '',
        to: '',
      },
    })
    expect(actions[1].target).toEqual({ tab: 'clients', clientFilter: 'codex-local' })
    expect(actions[2].target).toEqual({ tab: 'policies', policyFilter: 'policy-risk-open' })
    expect(actions[3].target).toEqual({ tab: 'grants', grantFilter: 'grant-risk-open' })
    expect(actions[4].target).toEqual({
      tab: 'audit',
      auditFilters: {
        actor: 'user-1',
        aiClientId: 'codex-local',
        toolName: 'delivery.actions.trigger',
        action: '',
        riskLevel: 'execute',
        result: '',
        from: '',
        to: '',
      },
    })

    const grantGuardrailActions = governanceFindingDrilldownActions({
      type: 'high_risk_grant_without_approval',
      severity: 'warning',
      summary: 'grant allows execute without approval',
      subjectType: 'role',
      subjectId: 'developer',
      aiClientId: 'codex-local',
      grantId: 'grant-risk-open',
      toolName: 'delivery.actions.trigger',
      riskLevel: 'execute',
    })
    expect(grantGuardrailActions.map((item) => item.label)).toEqual(['查看 client', '查看 grant', '补 guardrail', '查日志'])
    expect(grantGuardrailActions[2].target).toMatchObject({
      tab: 'policies',
      grantFilter: 'grant-risk-open',
      policyDraft: {
        name: 'Gateway grant approval guardrail',
        subjectType: 'role',
        subjectId: 'developer',
        aiClientId: 'codex-local',
        toolPatterns: ['delivery.actions.trigger'],
        riskLevels: ['execute'],
        approvalMode: 'require_approval',
      },
    })
  })

  it('maps governance token findings to PAT filters and service-token revoke actions', () => {
    const rows = governanceTokenFindingRows({
      expiredActive: [{
        kind: 'service_account_token',
        id: 'sat-expired',
        name: 'deploy-runner',
        ownerId: 'svc-delivery',
        tokenPrefix: 'sat_abc',
        severity: 'critical',
        message: 'active token is expired',
        expiresAt: '2026-05-01T00:00:00Z',
        daysUntilDue: -29,
      }],
      expiringSoon: [{
        kind: 'personal_access_token',
        id: 'pat-soon',
        name: 'local-codex',
        ownerId: 'user-1',
        tokenPrefix: 'pat_xyz',
        severity: 'warning',
        message: 'token expires soon',
        expiresAt: '2026-06-01T00:00:00Z',
        daysUntilDue: 2,
      }],
      stale: [],
      neverUsed: [{
        kind: 'personal_access_token',
        id: 'pat-never',
        name: 'unused',
        ownerId: 'user-2',
        tokenPrefix: 'pat_unused',
        severity: 'warning',
        message: 'token has never been used',
        staleDays: 90,
      }],
    })

    expect(rows.map((item) => item.key)).toEqual([
      'expiredActive:service_account_token:sat-expired',
      'expiringSoon:personal_access_token:pat-soon',
      'neverUsed:personal_access_token:pat-never',
    ])
    expect(governanceTokenFindingDrilldown(rows[0])).toEqual({
      tab: 'service-accounts',
      serviceTokenFilter: 'sat-expired',
      serviceTokenRevokeId: 'sat-expired',
    })
    expect(governanceTokenFindingDrilldown(rows[1])).toEqual({
      tab: 'tokens',
      tokenFilter: 'pat-soon',
    })
  })
})
