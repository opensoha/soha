import { OperationalPlanModal } from '@/components/operational-plan-modal'
import { ReadFeedback } from '@/components/request-feedback'
import type { ServiceFormValues } from '@/features/platform/resource-creation/forms'
import {
  getResourceFormDefinition,
  ServiceEditorForm,
} from '@/features/platform/resource-creation/forms'
import { planResourceUpdate } from '@/features/platform/shared/resource-update-plan'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { toScopeKey } from '@/types'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Modal, Spin } from 'antd'
import { useId, useMemo, useState } from 'react'
import { parse, stringify } from 'yaml'
import { useNetworkEditSource } from '../shared/use-network-edit-source'
import { serviceMutations } from './mutations'
import { mergeServiceManifest, serviceFormValuesFromManifest } from './service-edit-model'
import type { Service } from './types'

const serviceFormDefinition = getResourceFormDefinition('Service')

type LoadedService = {
  error?: string
  manifest?: unknown
  values?: ServiceFormValues
}

export function ServiceEditModal({
  clusterId,
  service,
  onClose,
}: {
  clusterId: string
  service: Service
  onClose: () => void
}) {
  const { message } = App.useApp()
  const formId = useId()
  const { localeCode } = useI18n()
  const isChinese = localeCode === 'zh_CN'
  const queryClient = useQueryClient()
  const scope = toScopeKey(clusterId, service.namespace)
  const changedMessage = isChinese
    ? 'Service 配置已改变。请关闭并重新打开编辑器，核对最新配置。'
    : 'Service configuration changed. Reopen the editor to review the latest configuration.'
  const {
    query: yamlQuery,
    source,
    sourceChanged,
    requireCurrentSource,
  } = useNetworkEditSource('services', { scope, name: service.name }, changedMessage)
  const updateMutation = usePlatformMutation(
    serviceMutations.updateYAML(queryClient, requireCurrentSource),
    {
      label: ['更新资源', 'Update resource'],
      target: platformResourceTarget,
    },
  )
  const platformWriteBlocked = updateMutation.blocked
  const planMutation = usePlatformMutation(
    {
      mutationFn: async (variables: Parameters<typeof planResourceUpdate>[0]) => {
        await requireCurrentSource()
        return planResourceUpdate(variables)
      },
    },
    {
      label: ['资源变更预检', 'Resource change preflight'],
      target: platformResourceTarget,
      readOnly: true,
    },
  )
  const [pendingContent, setPendingContent] = useState<string | null>(null)
  const loaded = useMemo<LoadedService>(() => {
    if (!source) return {}
    try {
      const manifest = parse(source)
      const values = serviceFormValuesFromManifest(manifest)
      if (values.name !== service.name || values.namespace !== service.namespace) {
        throw new Error('Service YAML identity does not match the selected resource')
      }
      return { manifest, values }
    } catch (error) {
      return { error: error instanceof Error ? error.message : String(error) }
    }
  }, [source, service.name, service.namespace])

  const error =
    yamlQuery.isError && !source
      ? yamlQuery.error.message
      : loaded.error || (!serviceFormDefinition ? 'Service form is unavailable' : undefined)
  const [formValues, setFormValues] = useState<ServiceFormValues | null>(null)
  const formDisabled =
    platformWriteBlocked ||
    sourceChanged ||
    yamlQuery.isError ||
    planMutation.isPending ||
    updateMutation.isPending ||
    Boolean(planMutation.data)

  function save(submitted: unknown) {
    if (!serviceFormDefinition || !loaded.manifest || formDisabled) return
    try {
      const lockedValues = {
        ...(submitted as ServiceFormValues),
        name: service.name,
        namespace: service.namespace,
      }
      const edited = serviceFormDefinition.buildManifest(lockedValues)
      const content = stringify(mergeServiceManifest(loaded.manifest, edited))
      setPendingContent(content)
      planMutation.mutate(
        { scope, kind: 'Service', name: service.name, content, requireObservedIdentity: true },
        {},
      )
    } catch (saveError) {
      void message.error(saveError instanceof Error ? saveError.message : String(saveError))
    }
  }

  function clearPlan() {
    if (platformWriteBlocked) return
    planMutation.reset()
    setPendingContent(null)
  }

  function applyUpdate() {
    if (!pendingContent || platformWriteBlocked || sourceChanged || yamlQuery.isError) return
    updateMutation.mutate(
      { scope, name: service.name, content: pendingContent, requireObservedIdentity: true },
      {
        onSuccess: () => {
          clearPlan()
        },
      },
    )
  }

  const close = () => {
    if (!platformWriteBlocked) onClose()
  }
  const success = updateMutation.isSuccess
  return (
    <>
      <Modal
        open
        title={isChinese ? `编辑 Service · ${service.name}` : `Edit Service · ${service.name}`}
        className="soha-service-editor"
        width="min(980px, calc(100vw - 32px))"
        style={{ top: 32 }}
        styles={{ body: { maxHeight: 'calc(100dvh - 200px)', overflowY: 'auto' } }}
        destroyOnHidden
        mask={{ closable: false }}
        closable={!platformWriteBlocked}
        keyboard={!platformWriteBlocked}
        onCancel={close}
        footer={
          success ? (
            <Button type="primary" onClick={onClose}>
              {isChinese ? '完成' : 'Done'}
            </Button>
          ) : (
            <>
              <Button onClick={close} disabled={platformWriteBlocked}>
                {isChinese ? '取消' : 'Cancel'}
              </Button>
              <Button
                type="primary"
                htmlType="submit"
                form={formId}
                disabled={formDisabled || !loaded.values || Boolean(error)}
                loading={planMutation.isPending || updateMutation.isPending}
              >
                {isChinese ? '预检更改' : 'Review changes'}
              </Button>
            </>
          )
        }
      >
        <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />
        {updateMutation.feedback}
        {planMutation.feedback}
        {!success && sourceChanged && <Alert showIcon type="warning" title={changedMessage} />}
        {success ? null : yamlQuery.isLoading ? (
          <div className="soha-resource-create-loading">
            <Spin size="large" />
          </div>
        ) : error ? (
          <Alert
            description={error}
            showIcon
            title={isChinese ? 'Service 配置加载失败' : 'Failed to load Service configuration'}
            type="error"
          />
        ) : loaded.values ? (
          <ServiceEditorForm
            id={formId}
            clusterId={clusterId || ''}
            disabled={formDisabled}
            localeCode={localeCode}
            onChange={setFormValues}
            onSubmit={save}
            value={formValues ?? loaded.values}
          />
        ) : null}
      </Modal>
      <OperationalPlanModal
        disabled={platformWriteBlocked || sourceChanged || yamlQuery.isError}
        feedback={
          <>
            <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />
            {updateMutation.feedback}
            {planMutation.feedback}
          </>
        }
        confirmText={isChinese ? '确认更新' : 'Confirm update'}
        loading={updateMutation.isPending}
        onCancel={clearPlan}
        onConfirm={applyUpdate}
        plan={planMutation.data ?? null}
        title={isChinese ? '确认资源变更' : 'Confirm resource change'}
      />
    </>
  )
}
