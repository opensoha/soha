import { useMutation, useQuery } from '@tanstack/react-query'
import { Alert, AutoComplete, Button, Card, Descriptions, Form, Space, Typography } from 'antd'
import type { NetworkAdmissionPreviewRequest } from '@opensoha/contracts/gen/ts/sohaapi'
import { ReadFeedback, requestErrorMessage, useInlineApiError } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'

export function NetworkAdmissionPreview() {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const permissions = usePermissionSnapshot()
  const can = (key: string) => hasPermission(readableQueryData(permissions)?.data, key)
  const devices = useQuery(
    networkAccessQueries.devices({ limit: 200 }, can('network_access.endpoint_devices.view')),
  )
  const bindings = useQuery(
    networkAccessQueries.nasBindings({ limit: 200 }, can('network_access.sites.view')),
  )
  const preview = useMutation(networkAccessMutations.policy.admissionPreview())
  const [form] = Form.useForm<NetworkAdmissionPreviewRequest>()
  const result = preview.data
  const deviceChoices = can('network_access.endpoint_devices.view')
    ? readableQueryData(devices)
    : undefined
  const bindingChoices = can('network_access.sites.view') ? readableQueryData(bindings) : undefined
  useInlineApiError(preview.error)

  return (
    <Card>
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          title={text('预览终端在指定网络上的接入等级', 'Preview endpoint admission on a network')}
          description={text(
            '使用终端登记用户、当前策略、健康信号和有效限制。预览不会授予访问；实际入网还需证书认证，并重新核实当前版本。',
            'Uses the enrolled owner, current policy, health and restrictions. Preview grants no access; admission still authenticates the certificate and rechecks current versions.',
          )}
        />
        <Form
          form={form}
          layout="vertical"
          disabled={preview.isPending}
          onValuesChange={() => preview.reset()}
          onFinish={(values) =>
            preview.mutate({
              deviceId: values.deviceId.trim(),
              nasBindingId: values.nasBindingId.trim(),
            })
          }
        >
          <Form.Item
            name="deviceId"
            label={text('终端', 'Endpoint')}
            rules={[{ required: true, whitespace: true }]}
          >
            <AutoComplete
              options={(deviceChoices ?? []).map((item) => ({
                value: item.id,
                label: `${item.name} · ${item.id}`,
              }))}
              filterOption={(input, option) =>
                String(option?.label).toLowerCase().includes(input.toLowerCase())
              }
              placeholder={text('选择终端或输入已知 ID', 'Choose an endpoint or enter a known ID')}
              maxLength={128}
            />
          </Form.Item>
          <Form.Item
            name="nasBindingId"
            label={text('接入网络', 'Admission network')}
            rules={[{ required: true, whitespace: true }]}
          >
            <AutoComplete
              options={(bindingChoices ?? []).map((item) => ({
                value: item.id,
                label: `${item.name} · ${item.ssid || item.id}`,
              }))}
              filterOption={(input, option) =>
                String(option?.label).toLowerCase().includes(input.toLowerCase())
              }
              placeholder={text(
                '选择 NAS 绑定或输入已知 ID',
                'Choose a NAS binding or enter a known ID',
              )}
              maxLength={128}
            />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={preview.isPending}>
            {text('预览准入', 'Preview admission')}
          </Button>
        </Form>
        <ReadFeedback query={devices} label={text('终端选项', 'Endpoint choices')} />
        <ReadFeedback query={bindings} label={text('接入网络选项', 'Admission network choices')} />
        {preview.isError && (
          <Alert
            type="error"
            showIcon
            title={text('准入预览失败', 'Admission preview failed')}
            description={requestErrorMessage(preview.error, localeCode)}
          />
        )}
        {result && (
          <>
            <Alert
              type={result.decision === 'allow' ? 'success' : 'error'}
              showIcon
              title={
                result.decision === 'allow'
                  ? text('当前策略允许此接入级别', 'Current policy permits this admission profile')
                  : text('当前策略拒绝接入', 'Current policy denies admission')
              }
              description={result.reasons.join(' · ')}
            />
            <Descriptions
              column={{ xs: 1, md: 2 }}
              items={[
                {
                  key: 'profile',
                  label: text('接入等级', 'Access profile'),
                  children: result.networkProfile || '—',
                },
                {
                  key: 'policy',
                  label: text('策略版本', 'Policy version'),
                  children: result.policyVersion,
                },
                {
                  key: 'time',
                  label: text('评估时间', 'Evaluated'),
                  children: formatDateTime(result.evaluatedAt),
                },
                {
                  key: 'attributes',
                  label: text('属性版本', 'Attribute version'),
                  children: result.attributeVersion ? (
                    <Typography.Text copyable style={{ wordBreak: 'break-all' }}>
                      {result.attributeVersion}
                    </Typography.Text>
                  ) : (
                    '—'
                  ),
                },
              ]}
            />
          </>
        )}
      </Space>
    </Card>
  )
}
