import { useState } from 'react'
import { Form, Modal, Select } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { deliveryQueries } from '../queries'
import type { WorkflowTemplate } from '../types'

export function UseWorkflowTemplateVersion({
  template,
  onClose,
}: {
  template: WorkflowTemplate
  onClose: () => void
}) {
  const navigate = useNavigate()
  const [applicationId, setApplicationId] = useState<string>()
  const [bindingId, setBindingId] = useState<string>()
  const applications = useQuery(deliveryQueries.applications.list())
  const detail = useQuery(deliveryQueries.applications.detail(applicationId ?? ''))
  return (
    <Modal
      open
      title={`使用 ${template.name} · v${template.publishedVersion}`}
      okText="配置环境工作流"
      okButtonProps={{
        disabled: !applicationId || !bindingId || applications.isError || detail.isError,
      }}
      onCancel={onClose}
      onOk={() => {
        if (
          applicationId &&
          bindingId &&
          !applications.isError &&
          !detail.isError &&
          readableQueryData(detail)?.bindings?.some(
            (item) => item.applicationEnvironmentId === bindingId,
          )
        )
          navigate(
            `/applications/${encodeURIComponent(applicationId)}/workflows/design?${new URLSearchParams({ bindingId, source: 'template', templateId: template.id, templateVersion: String(template.publishedVersion) })}`,
          )
      }}
    >
      <ReadFeedback query={applications} label="应用选项" />
      <ReadFeedback query={detail} label="应用环境" />
      <Form layout="vertical">
        <Form.Item label="应用" required>
          <Select
            aria-label="使用模板的应用"
            showSearch={{ optionFilterProp: 'label' }}
            value={applicationId}
            loading={applications.isLoading}
            options={readableQueryData(applications)
              ?.filter((app) => app.enabled)
              .map((app) => ({ value: app.id, label: app.name }))}
            onChange={(value) => {
              setApplicationId(value)
              setBindingId(undefined)
            }}
          />
        </Form.Item>
        <Form.Item label="环境" required>
          <Select
            aria-label="使用模板的环境"
            value={bindingId}
            loading={detail.isLoading}
            disabled={!applicationId}
            options={readableQueryData(detail)?.bindings?.map((binding) => ({
              value: binding.applicationEnvironmentId,
              label: binding.environmentName || binding.environmentKey || binding.environmentId,
            }))}
            onChange={setBindingId}
          />
        </Form.Item>
      </Form>
    </Modal>
  )
}
