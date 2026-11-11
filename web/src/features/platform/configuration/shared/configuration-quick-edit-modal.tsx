import { OperationalPlanModal } from '@/components/operational-plan-modal'
import { ReadFeedback } from '@/components/request-feedback'
import { StepFormModal } from '@/components/step-form-modal'
import { planResourceUpdate } from '@/features/platform/shared/resource-update-plan'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Col, Form, Input, InputNumber, Row, Select, Spin } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { parse, stringify } from 'yaml'
import {
  configurationQuickEditValuesFromManifest,
  mergeConfigurationQuickEditManifest,
  type ConfigurationQuickEditValues,
  type QuickEditableConfigurationKind,
} from './configuration-quick-edit-model'
import { configurationMutations } from './mutations'
import { configurationQueries } from './queries'

interface ConfigurationQuickEditModalProps {
  kind: QuickEditableConfigurationKind
  name: string
  namespace: string
  onClose: () => void
}

const kindLabels: Record<QuickEditableConfigurationKind, string> = {
  hpas: 'HorizontalPodAutoscaler',
  poddisruptionbudgets: 'PodDisruptionBudget',
}

export function ConfigurationQuickEditModal({
  kind,
  name,
  namespace,
  onClose,
}: ConfigurationQuickEditModalProps) {
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const scope = toScopeKey(clusterId, namespace)
  const yamlQuery = useQuery(configurationQueries.yaml(kind, scope, name))
  const visibleYamlQueryData = readableQueryData(yamlQuery)
  const updateMutation = usePlatformMutation(configurationMutations.updateYAML(kind, queryClient), {
    label: ['更新资源', 'Update resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = updateMutation.blocked
  const planMutation = usePlatformMutation(
    { mutationFn: planResourceUpdate },
    {
      label: ['资源变更预检', 'Resource change preflight'],
      target: platformResourceTarget,
      readOnly: true,
    },
  )
  const [pendingContent, setPendingContent] = useState<string | null>(null)
  const [form] = Form.useForm<ConfigurationQuickEditValues>()
  const loaded = useMemo(() => {
    if (!visibleYamlQueryData) return {}
    try {
      const manifest = parse(visibleYamlQueryData.content)
      return {
        manifest,
        values: configurationQuickEditValuesFromManifest(manifest, kind),
      }
    } catch (error) {
      return { error: error instanceof Error ? error.message : String(error) }
    }
  }, [kind, visibleYamlQueryData])

  const initialized = useRef('')
  const identity = JSON.stringify([clusterId, namespace, kind, name])
  useEffect(() => {
    if (initialized.current === identity) return
    if (loaded.values) {
      form.setFieldsValue(loaded.values)
      initialized.current = identity
    }
  }, [form, identity, loaded.values])

  const title = `${localeCode === 'zh_CN' ? '编辑' : 'Edit'} ${kindLabels[kind]} · ${name}`
  if (updateMutation.isSuccess)
    return (
      <StepFormModal
        open
        title={title}
        width={680}
        onClose={onClose}
        feedback={updateMutation.feedback}
      >
        <Button type="primary" onClick={onClose}>
          {localeCode === 'zh_CN' ? '完成' : 'Done'}
        </Button>
      </StepFormModal>
    )

  const error = yamlQuery.isError && !visibleYamlQueryData ? yamlQuery.error.message : loaded.error
  if (yamlQuery.isLoading || error || !loaded.manifest || !loaded.values) {
    return (
      <StepFormModal
        onClose={() => {
          if (!platformWriteBlocked) onClose?.()
        }}
        open
        title={title}
        width={680}
      >
        {yamlQuery.isLoading ? (
          <div className="soha-resource-create-loading">
            <Spin size="large" />
          </div>
        ) : (
          <Alert
            description={error || 'Resource manifest is unavailable'}
            showIcon
            title={localeCode === 'zh_CN' ? '配置加载失败' : 'Failed to load configuration'}
            type="error"
          />
        )}
      </StepFormModal>
    )
  }

  const clearPlan = () => {
    if (platformWriteBlocked) return
    planMutation.reset()
    setPendingContent(null)
  }
  const applyUpdate = () => {
    if (!pendingContent || platformWriteBlocked || yamlQuery.isError) return
    updateMutation.mutate(
      { scope, name, content: pendingContent },
      {
        onSuccess: () => {
          clearPlan()
        },
      },
    )
  }

  return (
    <>
      <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />

      <StepFormModal
        disabled={platformWriteBlocked || Boolean(yamlQuery.error)}
        feedback={
          <>
            <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />
            {updateMutation.feedback}
            {planMutation.feedback}
          </>
        }
        contentMaxWidth={560}
        current={0}
        form={form}
        initialValues={loaded.values}
        loading={planMutation.isPending || updateMutation.isPending}
        onClose={() => {
          if (!platformWriteBlocked) onClose?.()
        }}
        onCurrentChange={() => undefined}
        onFinish={(values) => {
          try {
            const content = stringify(
              mergeConfigurationQuickEditManifest(loaded.manifest, kind, values),
            )
            setPendingContent(content)
            planMutation.mutate({ scope, kind: kindLabels[kind], name, content }, {})
          } catch (saveError) {
            void message.error(saveError instanceof Error ? saveError.message : String(saveError))
          }
        }}
        open
        steps={[
          {
            title: localeCode === 'zh_CN' ? '配置' : 'Configuration',
            children:
              kind === 'hpas' ? (
                <Row gutter={16}>
                  <Col xs={24} md={12}>
                    <Form.Item label="最小副本数" name="minReplicas" rules={[{ required: true }]}>
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item label="最大副本数" name="maxReplicas" rules={[{ required: true }]}>
                      <InputNumber min={1} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                </Row>
              ) : (
                <Row gutter={16}>
                  <Col xs={24} md={12}>
                    <Form.Item label="可用性规则" name="availabilityMode">
                      <Select
                        options={[
                          { label: 'Min Available', value: 'minAvailable' },
                          { label: 'Max Unavailable', value: 'maxUnavailable' },
                        ]}
                      />
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="数值或百分比"
                      name="availabilityValue"
                      rules={[{ required: true }]}
                    >
                      <Input placeholder="1 / 25%" />
                    </Form.Item>
                  </Col>
                </Row>
              ),
          },
        ]}
        submitText={localeCode === 'zh_CN' ? '保存更改' : 'Save changes'}
        title={title}
        width={680}
      />
      <OperationalPlanModal
        disabled={platformWriteBlocked || Boolean(yamlQuery.error)}
        feedback={
          <>
            <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />
            {updateMutation.feedback}
            {planMutation.feedback}
          </>
        }
        confirmText={localeCode === 'zh_CN' ? '确认更新' : 'Confirm update'}
        loading={updateMutation.isPending}
        onCancel={clearPlan}
        onConfirm={applyUpdate}
        plan={planMutation.data ?? null}
        title={localeCode === 'zh_CN' ? '确认资源变更' : 'Confirm resource change'}
      />
    </>
  )
}
