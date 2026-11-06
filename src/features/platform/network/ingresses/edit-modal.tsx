import { OperationalPlanModal } from '@/components/operational-plan-modal'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
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
import { stringify } from 'yaml'
import { useNetworkEditSource } from '../shared/use-network-edit-source'
import { networkMutations } from '../shared/mutations'
import { readIngressManifest } from './configuration'
import { ingressEditValues, mergeIngressEdit, type IngressEditValues } from './edit-model'
import { IngressEditorForm } from './editor-form'
import type { Ingress } from './types'
import './styles.css'

export function IngressEditModal({
  clusterId,
  ingress,
  onClose,
}: {
  clusterId: string
  ingress: Ingress
  onClose: () => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN',
    { message } = App.useApp(),
    client = useQueryClient(),
    formId = useId()
  const scope = toScopeKey(clusterId, ingress.namespace)
  const changedMessage = zh
    ? 'Ingress 配置已改变。请关闭并重新打开编辑器，核对最新配置。'
    : 'Ingress configuration changed. Reopen the editor to review the latest configuration.'
  const { query, source, sourceChanged, requireCurrentSource } = useNetworkEditSource(
    'ingresses',
    { scope, name: ingress.name },
    changedMessage,
  )
  const loaded = useMemo(() => {
    if (!source) return {}
    try {
      return {
        values: ingressEditValues(readIngressManifest(source, ingress.name, ingress.namespace)),
      }
    } catch (e) {
      return { error: e instanceof Error ? e.message : String(e) }
    }
  }, [source, ingress.name, ingress.namespace])
  const update = usePlatformMutation(
    networkMutations.updateYAML('ingresses', client, requireCurrentSource),
    {
      label: ['更新 Ingress', 'Update Ingress'],
      target: platformResourceTarget,
    },
  )
  const plan = usePlatformMutation(
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
  const [pending, setPending] = useState<string | null>(null)
  const blocked = update.blocked,
    formDisabled =
      blocked ||
      sourceChanged ||
      query.isError ||
      plan.isPending ||
      Boolean(plan.data) ||
      update.isPending
  const close = () => {
    if (!blocked) onClose()
  }
  const clearPlan = () => {
    if (!blocked) {
      plan.reset()
      setPending(null)
    }
  }
  const submit = (values: IngressEditValues) => {
    if (!source || formDisabled) return
    try {
      const content = stringify(mergeIngressEdit(source, ingress.name, ingress.namespace, values))
      setPending(content)
      plan.mutate({
        scope,
        kind: 'Ingress',
        name: ingress.name,
        content,
        requireObservedIdentity: true,
      })
    } catch (e) {
      void message.error(e instanceof Error ? e.message : String(e))
    }
  }
  return (
    <>
      <Modal
        open
        title={zh ? `编辑 Ingress · ${ingress.name}` : `Edit Ingress · ${ingress.name}`}
        className="soha-ingress-editor"
        width="min(1040px, calc(100vw - 32px))"
        style={{ top: 32 }}
        styles={{ body: { maxHeight: 'calc(100dvh - 200px)', overflowY: 'auto' } }}
        mask={{ closable: false }}
        keyboard={!blocked}
        closable={!blocked}
        onCancel={close}
        destroyOnHidden
        footer={
          update.isSuccess ? (
            <Button type="primary" onClick={onClose}>
              {zh ? '完成' : 'Done'}
            </Button>
          ) : (
            <>
              <Button disabled={blocked} onClick={close}>
                {zh ? '取消' : 'Cancel'}
              </Button>
              <Button
                type="primary"
                htmlType="submit"
                form={formId}
                disabled={formDisabled || !loaded.values}
                loading={plan.isPending || update.isPending}
              >
                {zh ? '预检更改' : 'Review changes'}
              </Button>
            </>
          )
        }
      >
        <ReadFeedback query={query} canRead={query.isEnabled} label="Ingress YAML" />
        {plan.feedback}
        {update.feedback}
        {!update.isSuccess && sourceChanged && (
          <Alert showIcon type="warning" title={changedMessage} />
        )}
        {update.isSuccess ? null : query.isLoading ? (
          <Spin />
        ) : loaded.error ? (
          <Alert
            showIcon
            type="error"
            title={zh ? 'Ingress 配置加载失败' : 'Ingress configuration could not be read'}
            description={loaded.error}
          />
        ) : loaded.values ? (
          <>
            <div className="soha-ingress-editor-identity">
              <MetadataTag label={ingress.namespace} tone="blue" />
              <span>{zh ? '名称与命名空间保持不变' : 'Name and namespace are fixed'}</span>
            </div>
            <IngressEditorForm
              id={formId}
              value={loaded.values}
              disabled={formDisabled}
              onSubmit={submit}
            />
          </>
        ) : null}
      </Modal>
      <OperationalPlanModal
        title={zh ? '确认资源变更' : 'Confirm resource change'}
        confirmText={zh ? '确认更新' : 'Confirm update'}
        plan={plan.data ?? null}
        disabled={blocked || sourceChanged || query.isError}
        loading={update.isPending}
        feedback={
          <>
            {plan.feedback}
            {update.feedback}
          </>
        }
        onCancel={clearPlan}
        onConfirm={() => {
          if (pending && !blocked && !sourceChanged && !query.isError)
            update.mutate(
              { scope, name: ingress.name, content: pending, requireObservedIdentity: true },
              { onSuccess: clearPlan },
            )
        }}
      />
    </>
  )
}
