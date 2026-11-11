import { ReadFeedback } from '@/components/request-feedback'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import type {
  NetworkVPNProfileConfig,
  NetworkVPNSelectionPolicyConfig,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import { Form, Input, InputNumber, Select, Switch } from 'antd'
import { networkAccessQueries } from './queries'
import { vpnDocuments, vpnKeys } from './vpn-api'
export const vpnPolicyDefaults: NetworkVPNSelectionPolicyConfig = {
  name: '',
  strategy: 'latency',
  providerOrder: [],
  providerPreference: 'prefer',
  maxLatencyMs: 2000,
  maxTimeoutPercent: 50,
  maxSampleAgeSeconds: 60,
  minSamples: 3,
  missingMeasurements: 'priority',
  maxAttempts: 3,
  retryCooldownSeconds: 30,
  failoverOnDisconnect: false,
  allowManualFallback: false,
}
export const vpnProfileDefaults: NetworkVPNProfileConfig = {
  name: '',
  siteId: '',
  networkSpaceId: '',
  mode: 'external_vpn',
  resourceIds: [],
  gatewayIds: [],
  selectionPolicyId: '',
  allowManualSelection: true,
  enabled: false,
  assignments: { userIds: [], teamIds: [], deviceIds: [] },
}
export function VPNProfileFields({ can }: { can: (key: string) => boolean }) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const sites = useQuery(
    networkAccessQueries.sites({ limit: 200 }, can('network_access.sites.view')),
  )
  const visibleSitesData = sites.isEnabled === false ? undefined : readableQueryData(sites)
  const spaces = useQuery(
    networkAccessQueries.spaces({ limit: 200 }, can('network_access.spaces.view')),
  )
  const visibleSpacesData = spaces.isEnabled === false ? undefined : readableQueryData(spaces)
  const gateways = useQuery(
    networkAccessQueries.gateways({ limit: 200 }, can('network_access.gateways.view')),
  )
  const visibleGatewaysData = gateways.isEnabled === false ? undefined : readableQueryData(gateways)
  const resources = useQuery(
    networkAccessQueries.resources({ limit: 200 }, can('network_access.resources.view')),
  )
  const visibleResourcesData =
    resources.isEnabled === false ? undefined : readableQueryData(resources)
  const devices = useQuery(
    networkAccessQueries.devices({ limit: 200 }, can('network_access.endpoint_devices.view')),
  )
  const visibleDevicesData = devices.isEnabled === false ? undefined : readableQueryData(devices)
  const policies = useQuery({
    queryKey: vpnKeys.documents('selection-policies'),
    queryFn: () => vpnDocuments('selection-policies'),
    enabled: can('network_access.vpn_selection_policies.view'),
  })
  const visiblePoliciesData = policies.isEnabled === false ? undefined : readableQueryData(policies)
  return (
    <>
      <ReadFeedback canRead={sites.isEnabled} query={sites} label="站点" />
      <ReadFeedback canRead={spaces.isEnabled} query={spaces} label="网络空间" />
      <ReadFeedback canRead={gateways.isEnabled} query={gateways} label="网关" />
      <ReadFeedback canRead={resources.isEnabled} query={resources} label="网络资源" />
      <ReadFeedback canRead={devices.isEnabled} query={devices} label="设备" />
      <ReadFeedback canRead={policies.isEnabled} query={policies} label="策略" />

      <Form.Item name="siteId" label={text('目标站点', 'Target site')} rules={[{ required: true }]}>
        <Select
          showSearch
          optionFilterProp="label"
          options={visibleSitesData?.map((s) => ({ value: s.id, label: s.name }))}
        />
      </Form.Item>
      <Form.Item
        name="networkSpaceId"
        label={text('目标网络', 'Target network')}
        rules={[{ required: true }]}
      >
        <Select
          showSearch
          optionFilterProp="label"
          options={visibleSpacesData?.map((s) => ({ value: s.id, label: s.name }))}
        />
      </Form.Item>
      <Form.Item name="mode" label={text('授权模式', 'Access mode')} rules={[{ required: true }]}>
        <Select
          options={[
            { value: 'external_vpn', label: 'VPN' },
            { value: 'external_vpn_ztna', label: 'VPN + ZTNA' },
            { value: 'external_direct_ztna', label: text('外部直接 ZTNA', 'Direct ZTNA') },
            { value: 'internal_ztna', label: text('内网受保护访问', 'Internal ZTNA') },
          ]}
        />
      </Form.Item>
      <Form.Item name="resourceIds" label={text('受保护资源', 'Protected resources')}>
        <Select
          mode="multiple"
          showSearch
          optionFilterProp="label"
          options={visibleResourcesData?.map((r) => ({ value: r.id, label: r.name }))}
        />
      </Form.Item>
      <Form.Item
        name="gatewayIds"
        label={text('允许入口', 'Allowed entrances')}
        rules={[{ required: true, type: 'array', min: 1, max: 32 }]}
      >
        <Select
          mode="multiple"
          showSearch
          optionFilterProp="label"
          options={visibleGatewaysData?.map((g) => ({ value: g.id, label: g.name }))}
        />
      </Form.Item>
      <Form.Item
        name="selectionPolicyId"
        label={text('Auto 策略', 'Auto policy')}
        rules={[{ required: true }]}
      >
        <Select
          options={visiblePoliciesData?.map((p) => ({ value: p.id, label: p.configuration.name }))}
        />
      </Form.Item>
      <Form.Item name={['assignments', 'userIds']} label={text('分配用户 ID', 'Assigned user IDs')}>
        <Select mode="tags" tokenSeparators={[',']} />
      </Form.Item>
      <Form.Item name={['assignments', 'teamIds']} label={text('分配团队 ID', 'Assigned team IDs')}>
        <Select mode="tags" tokenSeparators={[',']} />
      </Form.Item>
      <Form.Item
        name={['assignments', 'deviceIds']}
        label={text(
          '设备限制（留空则不限设备）',
          'Device restriction (empty allows assigned owners’ devices)',
        )}
      >
        <Select
          mode="multiple"
          showSearch
          optionFilterProp="label"
          options={visibleDevicesData?.map((d) => ({ value: d.id, label: d.name || d.id }))}
        />
      </Form.Item>
      <Form.Item
        name="allowManualSelection"
        valuePropName="checked"
        label={text('允许手动选入口', 'Allow manual entrance selection')}
      >
        <Switch />
      </Form.Item>
      <Form.Item name="enabled" valuePropName="checked" label={text('启用方案', 'Enable profile')}>
        <Switch />
      </Form.Item>
    </>
  )
}
export function VPNPolicyFields() {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  return (
    <>
      <Form.Item name="strategy" label={text('排序模板', 'Ranking')} rules={[{ required: true }]}>
        <Select
          options={[
            { value: 'latency', label: text('入口延迟优先', 'Entrance latency first') },
            { value: 'provider', label: text('供应商优先', 'Provider first') },
            { value: 'priority', label: text('管理员优先级', 'Admin priority') },
          ]}
        />
      </Form.Item>
      <Form.Item
        name="providerOrder"
        label={text('供应商顺序', 'Provider order')}
        extra={text('按输入顺序排列供应商编码', 'Provider codes are ranked in entry order')}
      >
        <Select mode="tags" tokenSeparators={[',']} />
      </Form.Item>
      <Form.Item name="providerPreference" label={text('供应商约束', 'Provider constraint')}>
        <Select
          options={[
            { value: 'prefer', label: text('偏好，允许回退', 'Prefer, allow fallback') },
            { value: 'require', label: text('硬限制', 'Require match') },
          ]}
        />
      </Form.Item>
      {(
        [
          {
            name: 'maxLatencyMs',
            zh: '最大入口 RTT（ms）',
            en: 'Maximum entrance RTT (ms)',
            min: 1,
            max: 10000,
          },
          {
            name: 'maxTimeoutPercent',
            zh: '最大探测失败率（%）',
            en: 'Maximum probe failure (%)',
            min: 0,
            max: 100,
          },
          {
            name: 'maxSampleAgeSeconds',
            zh: '样本有效期（秒）',
            en: 'Sample freshness (seconds)',
            min: 5,
            max: 300,
          },
          {
            name: 'minSamples',
            zh: '最小成功样本数',
            en: 'Minimum successful samples',
            min: 1,
            max: 10,
          },
          { name: 'maxAttempts', zh: '最多尝试次数', en: 'Maximum attempts', min: 1, max: 5 },
          {
            name: 'retryCooldownSeconds',
            zh: '故障重试冷却（秒）',
            en: 'Failure retry cooldown (seconds)',
            min: 5,
            max: 300,
          },
        ] as const
      ).map((f) => (
        <Form.Item key={f.name} name={f.name} label={text(f.zh, f.en)} rules={[{ required: true }]}>
          <InputNumber min={f.min} max={f.max} precision={0} />
        </Form.Item>
      ))}
      <Form.Item name="missingMeasurements" label={text('缺失测量时', 'Missing measurements')}>
        <Select
          options={[
            {
              value: 'priority',
              label: text('按健康入口优先级回退', 'Fall back to healthy gateway priority'),
            },
            { value: 'deny', label: text('拒绝连接', 'Deny connection') },
          ]}
        />
      </Form.Item>
      <Form.Item
        name="failoverOnDisconnect"
        valuePropName="checked"
        label={text('故障后重新授权并重选', 'Reauthorize and select again on failure')}
      >
        <Switch />
      </Form.Item>
      <Form.Item
        name="allowManualFallback"
        valuePropName="checked"
        label={text('手动入口失败可转 Auto', 'Allow manual failure to fall back to Auto')}
      >
        <Switch />
      </Form.Item>
    </>
  )
}
export function VPNNameField() {
  const { t } = useI18n()
  return (
    <Form.Item name="name" label={t('common.name', '名称')} rules={[{ required: true, max: 200 }]}>
      <Input maxLength={200} />
    </Form.Item>
  )
}
