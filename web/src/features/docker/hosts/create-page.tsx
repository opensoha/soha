import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeMutationFeedback } from '@/features/compute'
import { useEffect, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Segmented,
  Select,
  Typography,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { scrollableModalBodyStyle } from '@/components/modal-styles'
import { virtualizationQueries } from '@/features/virtualization'
import { formatDateTime } from '@/utils/time'
import { localeText, useI18n } from '@/i18n'
import { dockerApi } from '../docker-api'
import { dockerQueries } from '../queries'
import type { DockerHost, DockerHostAgentInstallation, DockerHostInput } from '../docker-types'
import { compactRecord, refreshDocker } from '../shared/ui'

type RuntimeHostFormValues = Omit<
  DockerHostInput,
  'agentId' | 'architecture' | 'composeVersion' | 'dockerVersion' | 'ipAddress' | 'status'
> & {
  connectionMode?: 'quick' | 'manual'
  memoryGiB?: number
  diskGiB?: number
}

export function buildRuntimeHostPayload(values: RuntimeHostFormValues): DockerHostInput {
  const { connectionMode: _connectionMode, memoryGiB, diskGiB, ...host } = values
  return compactRecord({
    ...host,
    memoryBytes: memoryGiB ? Math.round(memoryGiB * 1024 ** 3) : values.memoryBytes,
    diskBytes: diskGiB ? Math.round(diskGiB * 1024 ** 3) : values.diskBytes,
  })
}

interface RuntimeHostModalProps {
  editing?: DockerHost | null
  onClose: () => void
  onSuccess?: () => void
  open: boolean
}

function runtimeHostFormValues(record: DockerHost): RuntimeHostFormValues {
  return {
    connectionMode: 'manual',
    name: record.name,
    endpoint: record.endpoint,
    environment: record.environment,
    owner: record.owner,
    team: record.team,
    virtualizationConnectionId: record.virtualizationConnectionId,
    vmId: record.vmId,
    vmName: record.vmName,
    cpuCoreCount: record.cpuCoreCount,
    memoryGiB: record.memoryBytes ? record.memoryBytes / 1024 ** 3 : undefined,
    diskGiB: record.diskBytes ? record.diskBytes / 1024 ** 3 : undefined,
    availablePortStart: record.availablePortStart,
    availablePortEnd: record.availablePortEnd,
  }
}

export function RuntimeHostModal({ editing, onClose, onSuccess, open }: RuntimeHostModalProps) {
  const [installation, setInstallation] = useState<DockerHostAgentInstallation | null>(null)
  const [form] = Form.useForm<RuntimeHostFormValues>()
  const connectionMode = Form.useWatch('connectionMode', form) ?? (editing ? 'manual' : 'quick')
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const virtualMachinesQuery = useQuery(virtualizationQueries.vms({ page: 1, pageSize: 500 }, open))
  const vmOptionsData = readableQueryData(virtualMachinesQuery)
  const virtualMachines = Array.isArray(vmOptionsData)
    ? vmOptionsData
    : (vmOptionsData?.items ?? [])
  const hostQuery = useQuery({
    ...dockerQueries.host(installation?.hostId ?? '', Boolean(installation)),
    refetchInterval: (query) => (hasAgentReport(query.state.data) ? false : 2_000),
  })
  const agentReported = hasAgentReport(readableQueryData(hostQuery))
  const saveMutation = useMutation({
    mutationFn: async (values: RuntimeHostFormValues) => {
      const host = editing
        ? await dockerApi.updateHost(editing.id, buildRuntimeHostPayload(values))
        : await dockerApi.createHost(buildRuntimeHostPayload(values))
      if (!editing && values.connectionMode !== 'manual') {
        try {
          return { host, installation: await dockerApi.createHostAgentInstallation(host.id) }
        } catch (error) {
          const reason = error instanceof Error ? error.message : String(error)
          try {
            await dockerApi.deleteHost(host.id)
          } catch (cleanupError) {
            const cleanupDetail =
              cleanupError instanceof Error ? cleanupError.message : String(cleanupError)
            throw Object.assign(
              new Error(
                localeText(
                  localeCode,
                  `${reason}；临时主机记录自动清理失败，请在主机列表中删除 ${host.name || host.id}。清理失败原因：${cleanupDetail}`,
                  `${reason}; failed to clean up the temporary host. Delete ${host.name || host.id} from the host list. Cleanup error: ${cleanupDetail}`,
                ),
              ),
              { cause: cleanupError },
            )
          }
          throw Object.assign(
            new Error(
              localeText(
                localeCode,
                `${reason}；临时主机记录已自动清理。`,
                `${reason}; the temporary host was removed.`,
              ),
            ),
            {
              cause: error,
            },
          )
        }
      }
      return {
        host,
        installation: undefined,
      }
    },
    onSuccess: (result) => {
      refreshDocker(queryClient)
      onSuccess?.()
      if (result.installation) {
        setInstallation(result.installation)
        message.success(localeText(localeCode, '安装命令已生成', 'Install command generated'))
      } else {
        message.success(
          editing
            ? localeText(localeCode, '运行时主机已更新', 'Runtime host updated')
            : localeText(
                localeCode,
                '运行时主机记录已保存，等待 Agent 上报',
                'Runtime host record saved; waiting for Agent report',
              ),
        )
      }
      form.resetFields()
      onClose()
    },
  })

  const closeForm = () => {
    if (saveMutation.isPending) return
    form.resetFields()
    saveMutation.reset()
    onClose()
  }

  useEffect(() => {
    if (agentReported) refreshDocker(queryClient)
  }, [agentReported, queryClient])

  const initialValues: RuntimeHostFormValues = editing
    ? runtimeHostFormValues(editing)
    : {
        connectionMode: 'quick',
        name: '',
        availablePortStart: 20000,
        availablePortEnd: 39999,
      }

  return (
    <>
      {!open ? (
        <ComputeMutationFeedback
          operations={[
            {
              label: localeText(localeCode, '保存运行时主机', 'Save runtime host'),
              mutation: saveMutation,
              target: saveMutation.variables?.name,
              receipt: {
                message: saveMutation.data?.installation
                  ? localeText(
                      localeCode,
                      '主机记录与安装命令已生成，等待 Agent 上报',
                      'Host record and install command generated; waiting for Agent report',
                    )
                  : localeText(
                      localeCode,
                      '主机记录已保存，运行状态以 Agent 上报为准',
                      'Host record saved; runtime state comes from Agent reports',
                    ),
              },
            },
          ]}
        />
      ) : null}
      <Modal
        title={
          editing
            ? localeText(localeCode, '编辑运行时主机', 'Edit runtime host')
            : localeText(localeCode, '接入运行时主机', 'Connect runtime host')
        }
        open={open}
        onCancel={closeForm}
        onOk={() => form.submit()}
        confirmLoading={saveMutation.isPending}
        okText={
          editing
            ? localeText(localeCode, '保存主机', 'Save host')
            : connectionMode === 'manual'
              ? localeText(localeCode, '接入主机', 'Connect host')
              : localeText(localeCode, '生成安装命令', 'Generate install command')
        }
        cancelText={localeText(localeCode, '取消', 'Cancel')}
        width={680}
        destroyOnHidden
        mask={{ closable: false }}
        style={{ top: 32 }}
        styles={{ body: { ...scrollableModalBodyStyle, maxHeight: 'calc(100dvh - 180px)' } }}
      >
        <ReadFeedback
          query={virtualMachinesQuery}
          label={localeText(localeCode, '虚拟机选项', 'VM options')}
        />
        <ComputeMutationFeedback
          operations={[
            {
              label: localeText(localeCode, '保存运行时主机', 'Save runtime host'),
              mutation: saveMutation,
              target: saveMutation.variables?.name,
            },
          ]}
        />
        <Form<RuntimeHostFormValues>
          form={form}
          layout="vertical"
          preserve={false}
          onFinish={(values) => saveMutation.mutate(values)}
          initialValues={initialValues}
        >
          {!editing ? (
            <Form.Item
              name="connectionMode"
              label={localeText(localeCode, '接入方式', 'Connection mode')}
              rules={[{ required: true }]}
            >
              <Segmented
                block
                options={[
                  {
                    label: localeText(localeCode, '快速安装 Agent', 'Quick Agent install'),
                    value: 'quick',
                  },
                  {
                    label: localeText(localeCode, '已有 Agent', 'Existing Agent'),
                    value: 'manual',
                  },
                ]}
              />
            </Form.Item>
          ) : null}
          <Form.Item
            name="name"
            label={localeText(localeCode, '名称', 'Name')}
            rules={[
              { required: true, message: localeText(localeCode, '请输入名称', 'Enter a name') },
            ]}
          >
            <Input />
          </Form.Item>
          {connectionMode === 'manual' ? (
            <Form.Item
              name="endpoint"
              label="Agent Endpoint"
              tooltip={localeText(
                localeCode,
                'Soha Server 可访问的 Soha Agent HTTP 地址。其余运行时信息会在 Agent 上报后自动更新。',
                'The Soha Agent HTTP address reachable by Soha Server. Other runtime details update after the Agent reports.',
              )}
              rules={[
                {
                  required: true,
                  message: localeText(
                    localeCode,
                    '请输入 Agent Endpoint',
                    'Enter the Agent endpoint',
                  ),
                },
                {
                  type: 'url',
                  message: localeText(
                    localeCode,
                    '请输入有效的 HTTP 或 HTTPS 地址',
                    'Enter a valid HTTP or HTTPS URL',
                  ),
                },
              ]}
            >
              <Input placeholder="http://10.0.0.10:18080" />
            </Form.Item>
          ) : (
            <Alert
              style={{ marginBottom: 16 }}
              showIcon
              type="info"
              title={localeText(
                localeCode,
                '保存后生成限时安装命令',
                'Generate a time-limited install command',
              )}
              description={localeText(
                localeCode,
                'Agent 上报后会自动填写 Endpoint、Agent ID、IP、架构以及 Docker/Compose 版本。',
                'Endpoint, Agent ID, IP, architecture, and Docker/Compose versions update after the Agent reports.',
              )}
            />
          )}

          <div className="soha-runtime-host-ownership-grid">
            <Form.Item name="environment" label={localeText(localeCode, '环境', 'Environment')}>
              <Input />
            </Form.Item>
            <Form.Item name="owner" label={localeText(localeCode, '负责人', 'Owner')}>
              <Input />
            </Form.Item>
            <Form.Item name="team" label={localeText(localeCode, '团队', 'Team')}>
              <Input />
            </Form.Item>
          </div>
          <Form.Item
            className="soha-runtime-host-vm-field"
            name="vmId"
            label={localeText(localeCode, '关联虚拟机', 'Virtual machine')}
            tooltip={localeText(
              localeCode,
              '关联到 Soha 已管理的虚拟机，用于从运行时主机返回虚拟机资源。裸机可不选择。',
              'Link a Soha-managed virtual machine for navigation from this host. Leave empty for bare metal.',
            )}
          >
            <Select
              allowClear
              loading={virtualMachinesQuery.isLoading}
              options={virtualMachines
                .filter((vm) => vm.status?.toLowerCase() !== 'deleted')
                .map((vm) => ({
                  label: [vm.name, vm.connectionName ?? vm.provider].filter(Boolean).join(' · '),
                  value: vm.id,
                }))}
              placeholder={localeText(
                localeCode,
                '选择 Soha 已管理的虚拟机',
                'Select a Soha-managed virtual machine',
              )}
              showSearch={{ optionFilterProp: 'label' }}
              onChange={(vmId) => {
                const vm = virtualMachines.find((item) => item.id === vmId)
                form.setFieldsValue({
                  virtualizationConnectionId: vm?.connectionId,
                  vmName: vm?.name,
                })
              }}
            />
          </Form.Item>
          <Form.Item name="virtualizationConnectionId" hidden>
            <Input />
          </Form.Item>
          <Form.Item name="vmName" hidden>
            <Input />
          </Form.Item>
          <div className="soha-runtime-host-capacity-grid">
            <Form.Item name="cpuCoreCount" label={localeText(localeCode, 'CPU 核数', 'CPU cores')}>
              <InputNumber min={1} precision={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="memoryGiB" label={localeText(localeCode, '内存 GiB', 'Memory GiB')}>
              <InputNumber min={1} precision={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item name="diskGiB" label={localeText(localeCode, '磁盘 GiB', 'Disk GiB')}>
              <InputNumber min={1} precision={0} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="availablePortStart"
              label={localeText(localeCode, '端口池起始', 'Port pool start')}
            >
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
            <Form.Item
              name="availablePortEnd"
              label={localeText(localeCode, '端口池结束', 'Port pool end')}
            >
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            </Form.Item>
          </div>
        </Form>
      </Modal>
      <Modal
        destroyOnHidden
        footer={
          <Button onClick={() => setInstallation(null)}>
            {localeText(localeCode, '关闭', 'Close')}
          </Button>
        }
        mask={{ closable: false }}
        open={Boolean(installation)}
        title={localeText(localeCode, '快速安装 Agent', 'Quick Agent install')}
        width={680}
        onCancel={() => setInstallation(null)}
      >
        <div className="space-y-4">
          <Alert
            showIcon
            type={agentReported ? 'success' : 'info'}
            title={
              agentReported
                ? localeText(localeCode, 'Agent 已上报', 'Agent reported')
                : localeText(localeCode, '等待 Agent 上报', 'Waiting for Agent')
            }
            description={
              agentReported
                ? localeText(
                    localeCode,
                    '运行时主机信息已自动更新。',
                    'Runtime host details have been updated.',
                  )
                : localeText(
                    localeCode,
                    '安装票据有效期内，在目标 Linux 主机执行下列命令。',
                    'Run this command on the target Linux host before the ticket expires.',
                  )
            }
          />
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <Typography.Text strong>
                {localeText(localeCode, '安装命令', 'Install command')}
              </Typography.Text>
              <Typography.Text
                copyable={{
                  text: installation?.command ?? '',
                  tooltips: [
                    localeText(localeCode, '复制命令', 'Copy command'),
                    localeText(localeCode, '已复制', 'Copied'),
                  ],
                }}
              >
                {localeText(localeCode, '复制', 'Copy')}
              </Typography.Text>
            </div>
            <Input.TextArea
              aria-label={localeText(localeCode, 'Agent 安装命令', 'Agent install command')}
              autoSize={{ minRows: 3, maxRows: 6 }}
              readOnly
              value={installation?.command ?? ''}
            />
            <Typography.Text type="secondary">
              {localeText(localeCode, '有效期至', 'Expires at')}{' '}
              {formatDateTime(installation?.expiresAt)}
            </Typography.Text>
          </div>
          {agentReported ? (
            <Descriptions
              bordered
              size="small"
              column={{ xs: 1, sm: 1, md: 2, lg: 2, xl: 2, xxl: 2 }}
              items={[
                {
                  key: 'agentId',
                  label: 'Agent ID',
                  children: readableQueryData(hostQuery)?.agentId || '-',
                },
                {
                  key: 'endpoint',
                  label: 'Endpoint',
                  children: readableQueryData(hostQuery)?.endpoint || '-',
                },
                {
                  key: 'ipAddress',
                  label: localeText(localeCode, 'IP 地址', 'IP address'),
                  children: readableQueryData(hostQuery)?.ipAddress || '-',
                },
                {
                  key: 'architecture',
                  label: localeText(localeCode, '架构', 'Architecture'),
                  children: readableQueryData(hostQuery)?.architecture || '-',
                },
                {
                  key: 'dockerVersion',
                  label: localeText(localeCode, 'Docker 版本', 'Docker version'),
                  children: readableQueryData(hostQuery)?.dockerVersion || '-',
                },
                {
                  key: 'composeVersion',
                  label: localeText(localeCode, 'Compose 版本', 'Compose version'),
                  children: readableQueryData(hostQuery)?.composeVersion || '-',
                },
              ]}
            />
          ) : null}
        </div>
      </Modal>
    </>
  )
}

function hasAgentReport(host: DockerHost | undefined): boolean {
  return Boolean(host?.agentId && host.endpoint && host.dockerVersion)
}
