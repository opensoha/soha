import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { UploadOutlined } from '@ant-design/icons'
import type {
  ObservabilityDashboardImportResult,
  ObservabilityGrafanaDashboardImportInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { UploadFile } from 'antd'
import { Alert, App, Button, Modal, Segmented, Select, Space, Typography, Upload } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import '../observability-pages.css'
import { observabilityDashboardMutations } from './mutations'
import { observabilityDashboardQueries } from './queries'
import { dashboardTemplateJSON, dashboardTemplates } from './templates'

const maxDashboardBytes = 2 * 1024 * 1024

export function ImportDashboardModal({
  onImported,
  onOpenChange,
  open,
}: {
  onImported: (result: ObservabilityDashboardImportResult) => void
  onOpenChange: (open: boolean) => void
  open: boolean
}) {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [fileList, setFileList] = useState<UploadFile[]>([])
  const [dataSourceId, setDataSourceId] = useState('')
  const [sourceMode, setSourceMode] = useState<'template' | 'json'>('template')
  const [templateKey, setTemplateKey] = useState('kubernetes-workloads')
  const dataSourcesQuery = useQuery({
    ...observabilityDashboardQueries.metricDataSources(),
    enabled: open,
  })
  const dataSources = readableQueryData(dataSourcesQuery) ?? []

  useEffect(() => {
    if (open && !dataSourceId && dataSources.length === 1) {
      setDataSourceId(dataSources[0].id)
    }
  }, [dataSourceId, dataSources, open])

  function reset() {
    setFileList([])
    setDataSourceId('')
    setSourceMode('template')
    setTemplateKey('kubernetes-workloads')
  }
  const preparingRef = useRef(false)
  const [preparing, setPreparing] = useState(false)
  const importMutation = useOperationMutation(observabilityDashboardMutations.import(queryClient), {
    label: ['导入仪表盘', 'Import dashboard'],
    target: (input) => input.dataSourceId,
    receipt: (result) => ({
      message: `已导入 ${result.importedPanelCount} 个面板`,
      evidence: result.dashboard.id,
    }),
  })
  const blocked = preparing || importMutation.blocked
  function close() {
    if (preparingRef.current || importMutation.blocked) return
    reset()
    onOpenChange(false)
  }
  async function submit() {
    if (
      preparingRef.current ||
      importMutation.blocked ||
      !dataSourceId ||
      !dataSources.some((item) => item.id === dataSourceId)
    )
      return
    preparingRef.current = true
    setPreparing(true)
    try {
      let json = dashboardTemplateJSON(templateKey)
      if (sourceMode === 'json') {
        const file = fileList[0]?.originFileObj
        if (!file) return
        if (file.size > maxDashboardBytes) {
          message.error('JSON 文件不能超过 2 MiB')
          return
        }
        try {
          json = await file.text()
        } catch {
          message.error('无法读取 JSON 文件')
          return
        }
      }
      if (!json) return
      const input: ObservabilityGrafanaDashboardImportInput = { json, dataSourceId }
      importMutation.mutate(input, {
        onSuccess: (result) => {
          reset()
          onOpenChange(false)
          onImported(result)
        },
      })
    } finally {
      preparingRef.current = false
      setPreparing(false)
    }
  }

  return (
    <Modal
      className="soha-observability-modal"
      style={{ top: 32 }}
      classNames={{
        body: 'soha-observability-modal-body',
        header: 'soha-observability-modal-header',
      }}
      styles={operationModalStyles}
      closable={!blocked}
      keyboard={!blocked}
      mask={{ closable: false }}
      cancelButtonProps={{ disabled: blocked }}
      destroyOnHidden
      open={open}
      title="添加仪表盘"
      okText="添加"
      cancelText="取消"
      confirmLoading={preparing || importMutation.isPending}
      okButtonProps={{
        'aria-label': '添加仪表盘',
        disabled:
          blocked ||
          queryAccessDenied(dataSourcesQuery) ||
          !dataSourceId ||
          (sourceMode === 'template' ? !dashboardTemplateJSON(templateKey) : fileList.length === 0),
      }}
      onCancel={close}
      onOk={submit}
    >
      <ReadFeedback label="指标数据源" query={dataSourcesQuery} />
      {importMutation.feedback}
      <Space className="soha-dashboard-import-fields" orientation="vertical" size={16}>
        <Segmented
          disabled={blocked}
          block
          options={[
            { label: 'Soha 模板', value: 'template' },
            { label: 'Grafana JSON', value: 'json' },
          ]}
          value={sourceMode}
          onChange={setSourceMode}
        />
        {dataSourcesQuery.isSuccess && dataSources.length === 0 ? (
          <Alert
            showIcon
            type="warning"
            title="没有可用的 Prometheus 数据源"
            action={
              <Button
                size="small"
                type="link"
                onClick={() => {
                  reset()
                  onOpenChange(false)
                  navigate('/ai-workbench/model-settings')
                }}
              >
                去配置
              </Button>
            }
          />
        ) : null}
        <Space className="soha-dashboard-import-field" orientation="vertical" size={6}>
          <Typography.Text strong>Prometheus 数据源</Typography.Text>
          <Select
            allowClear
            className="soha-dashboard-import-control"
            disabled={blocked}
            loading={dataSourcesQuery.isLoading}
            options={dataSources.map((item) => ({ label: item.name, value: item.id }))}
            placeholder="选择数据源"
            value={dataSourceId || undefined}
            onChange={(value) => setDataSourceId(value ?? '')}
          />
        </Space>
        {sourceMode === 'template' ? (
          <Space className="soha-dashboard-import-field" orientation="vertical" size={6}>
            <Typography.Text strong>模板</Typography.Text>
            <Select
              disabled={blocked}
              className="soha-dashboard-import-control"
              options={dashboardTemplates.map((item) => ({
                disabled: !item.available,
                label: item.available ? item.name : `${item.name}（未就绪）`,
                title: item.reason ?? item.description,
                value: item.key,
              }))}
              value={templateKey}
              onChange={setTemplateKey}
            />
            <Typography.Text type="secondary">
              {dashboardTemplates.find((item) => item.key === templateKey)?.description}
            </Typography.Text>
          </Space>
        ) : (
          <Space className="soha-dashboard-import-field" orientation="vertical" size={6}>
            <Typography.Text strong>Grafana JSON</Typography.Text>
            <Upload
              disabled={blocked}
              accept="application/json,.json"
              beforeUpload={() => false}
              fileList={fileList}
              maxCount={1}
              onChange={({ fileList: nextFiles }) => setFileList(nextFiles.slice(-1))}
            >
              <Button icon={<UploadOutlined />}>选择 JSON</Button>
            </Upload>
          </Space>
        )}
      </Space>
    </Modal>
  )
}
