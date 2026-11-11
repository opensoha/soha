import { Alert, Button, Collapse, Form, Input, InputNumber, Select, Space, Typography } from 'antd'
import type {
  DeploymentTemplateHelmSource,
  HelmDeliveryConfiguration,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useDeferredValue, useId, useState } from 'react'
import { getHelmChartVersionOptions, kubernetesHelmQueries } from '@/features/platform'
import type { HelmChart } from '@/types/platform'
import { deliveryApi } from './api'
import { parse, stringify } from 'yaml'

export function parseHelmValues(value: unknown): DeploymentTemplateHelmSource['values'] {
  const parsed =
    typeof value === 'string'
      ? parse(value.trim() || '{}', { maxAliasCount: 50, uniqueKeys: true })
      : (value ?? {})
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
    throw new Error('Values 需要是 YAML 对象')
  JSON.stringify(parsed, (_key, item: unknown) => {
    if (typeof item === 'number' && !Number.isFinite(item))
      throw new Error('Values 数值必须是有限数字')
    return item
  })
  return parsed
}

export function parseHelmSource(
  source: DeploymentTemplateHelmSource,
): DeploymentTemplateHelmSource {
  return {
    ...source,
    digest: source.digest?.trim() || undefined,
    values: parseHelmValues(source.values),
  }
}

export function HelmValuesField({
  name,
  label = 'Values 覆盖',
  tooltip,
  schema,
}: {
  name: (string | number)[]
  label?: string
  tooltip?: string
  schema?: Record<string, unknown>
}) {
  return (
    <Form.Item
      name={name}
      label={label}
      tooltip={tooltip}
      getValueProps={(value: unknown) => ({
        value: typeof value === 'string' ? value : stringify(value ?? {}),
      })}
      rules={[
        {
          validator: async (_, value: unknown) => {
            parseHelmValues(value)
          },
        },
      ]}
    >
      <HelmValuesEditor schema={schema} />
    </Form.Item>
  )
}

function HelmValuesEditor({
  value = '',
  onChange,
  schema,
  id,
}: {
  value?: string
  onChange?: (value: string) => void
  schema?: Record<string, unknown>
  id?: string
}) {
  const fieldId = useId()
  let values: Record<string, unknown> | undefined
  try {
    values = parseHelmValues(value)
  } catch {
    // The owning Form.Item reports YAML errors; keep its text editable.
  }
  const properties = schema?.properties
  const fields =
    properties && typeof properties === 'object' && !Array.isArray(properties)
      ? (Object.entries(properties).filter(
          ([, field]) =>
            field &&
            typeof field === 'object' &&
            ['string', 'boolean', 'number', 'integer'].includes(field.type),
        ) as [string, Record<string, unknown>][])
      : []
  const update = (key: string, next: unknown) => {
    if (!values) return
    onChange?.(
      stringify(
        Object.fromEntries([
          ...Object.entries(values).filter(([name]) => name !== key),
          ...(next === undefined || next === null ? [] : [[key, next]]),
        ]),
      ),
    )
  }
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Input.TextArea
        id={id}
        value={value}
        onChange={(event) => onChange?.(event.target.value)}
        autoSize={{ minRows: 4, maxRows: 16 }}
        spellCheck={false}
      />
      {fields.length ? (
        <Collapse
          items={[
            {
              key: 'fields',
              label: '按 Schema 编辑覆盖字段',
              children: (
                <>
                  <Typography.Paragraph type="secondary">
                    未填写的字段沿用已有值；完整 Values 在部署预检时校验。
                  </Typography.Paragraph>
                  {fields.map(([key, field], index) => {
                    const inputId = `${fieldId}-${index}`
                    const current = values?.[key]
                    const numeric = field.type === 'number' || field.type === 'integer'
                    const options =
                      field.enum ?? (field.type === 'boolean' ? [true, false] : undefined)
                    return (
                      <Form.Item
                        key={key}
                        label={String(field.title || key)}
                        htmlFor={inputId}
                        tooltip={
                          typeof field.description === 'string' ? field.description : undefined
                        }
                      >
                        {Array.isArray(options) ? (
                          <Select
                            id={inputId}
                            allowClear
                            disabled={!values}
                            value={current === undefined ? undefined : JSON.stringify(current)}
                            options={options
                              .filter(
                                (item) =>
                                  typeof item === 'string' ||
                                  typeof item === 'number' ||
                                  typeof item === 'boolean',
                              )
                              .map((item) => ({
                                value: JSON.stringify(item),
                                label: String(item),
                              }))}
                            onChange={(next) =>
                              update(key, next === undefined ? undefined : JSON.parse(next))
                            }
                          />
                        ) : numeric ? (
                          <InputNumber
                            id={inputId}
                            disabled={!values}
                            value={typeof current === 'number' ? current : null}
                            min={typeof field.minimum === 'number' ? field.minimum : undefined}
                            max={typeof field.maximum === 'number' ? field.maximum : undefined}
                            precision={field.type === 'integer' ? 0 : undefined}
                            onChange={(next) => update(key, next)}
                          />
                        ) : (
                          <Input
                            id={inputId}
                            disabled={!values}
                            allowClear
                            value={typeof current === 'string' ? current : ''}
                            maxLength={
                              typeof field.maxLength === 'number' ? field.maxLength : undefined
                            }
                            onChange={(event) => update(key, event.target.value)}
                          />
                        )}
                        {Object.prototype.hasOwnProperty.call(values ?? {}, key) ? (
                          <Button size="small" onClick={() => update(key, undefined)}>
                            移除覆盖
                          </Button>
                        ) : null}
                      </Form.Item>
                    )
                  })}
                </>
              ),
            },
          ]}
        />
      ) : null}
    </Space>
  )
}

export function HelmSourceFields({
  name,
  clusterId,
}: {
  name: (string | number)[]
  clusterId?: string
}) {
  const form = Form.useFormInstance()
  return (
    <>
      {clusterId ? (
        <Collapse
          items={[
            {
              key: 'catalog',
              label: '从公共 Chart 目录选择',
              children: (
                <HelmChartPicker
                  clusterId={clusterId}
                  onSelect={(source) => form.setFieldValue(name, source)}
                />
              ),
            },
          ]}
        />
      ) : null}
      <Form.Item
        name={[...name, 'repositoryUrl']}
        label="Chart 仓库"
        rules={[{ required: true }]}
        tooltip="HTTPS 仓库地址或 OCI 仓库路径。"
      >
        <Input placeholder="https://charts.example.com 或 oci://registry.example.com/charts" />
      </Form.Item>
      <Form.Item name={[...name, 'chart']} label="Chart 名称" rules={[{ required: true }]}>
        <Input />
      </Form.Item>
      <Form.Item name={[...name, 'version']} label="Chart 固定版本" rules={[{ required: true }]}>
        <Input placeholder="1.2.3" />
      </Form.Item>
      <Form.Item
        name={[...name, 'digest']}
        label="Chart SHA256"
        rules={[{ pattern: /^sha256:[a-f0-9]{64}$/, message: '请输入完整 sha256 摘要' }]}
      >
        <Input allowClear />
      </Form.Item>
      <Collapse
        items={[
          {
            key: 'credentials',
            label: '私有仓库凭据',
            children: (
              <>
                {['CHART_USERNAME', 'CHART_PASSWORD', 'CHART_CA_CERT'].map((key) => (
                  <Form.Item
                    key={key}
                    name={[...name, 'secretRefs', key]}
                    label={key}
                    tooltip="填写已授权给本应用的 Secret 引用。"
                    normalize={(value: string) => value.trim() || undefined}
                    rules={[
                      { pattern: /^soha:\/\/secrets\//, message: '请使用 soha://secrets/ 引用' },
                    ]}
                  >
                    <Input placeholder="soha://secrets/..." />
                  </Form.Item>
                ))}
              </>
            ),
          },
        ]}
      />
      <HelmValuesField
        name={[...name, 'values']}
        tooltip="只填需要覆盖的值；留空对象使用 Chart 默认值。敏感值使用 soha://secrets/ 引用。"
      />
    </>
  )
}

function HelmChartPicker({
  clusterId,
  onSelect,
}: {
  clusterId: string
  onSelect: (source: DeploymentTemplateHelmSource) => void
}) {
  const [keyword, setKeyword] = useState('')
  const search = useDeferredValue(keyword.trim())
  const [chart, setChart] = useState<HelmChart | null>(null)
  const [version, setVersion] = useState('')
  const query = useQuery(
    kubernetesHelmQueries.chartCatalog({ clusterId, keyword: search, limit: 30, offset: 0 }),
  )
  const detail = useQuery(
    kubernetesHelmQueries.chartDetail(
      chart
        ? {
            clusterId,
            repositoryName: chart.repositoryName || '',
            chartName: chart.name,
            version,
          }
        : null,
    ),
  )
  const error = query.error || detail.error
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Input
        aria-label="搜索公共 Chart"
        placeholder="搜索 Chart 名称或描述"
        value={keyword}
        onChange={(event) => setKeyword(event.target.value)}
        allowClear
      />
      {error ? <Alert type="error" title={error.message} showIcon /> : null}
      <Select
        aria-label="公共 Chart"
        style={{ width: '100%' }}
        loading={query.isLoading}
        value={chart ? `${chart.repositoryName}/${chart.name}` : undefined}
        options={(query.data?.charts ?? []).map((item) => ({
          value: `${item.repositoryName}/${item.name}`,
          label: `${item.repositoryDisplay || item.repositoryName} / ${item.name}`,
        }))}
        onChange={(key) => {
          const item = query.data?.charts.find(
            (candidate) => `${candidate.repositoryName}/${candidate.name}` === key,
          )
          setChart(item ?? null)
          setVersion(item?.latestVersion || '')
        }}
      />
      {chart ? (
        <>
          <Select
            aria-label="公共 Chart 版本"
            style={{ minWidth: 160 }}
            loading={detail.isLoading}
            value={version}
            onChange={setVersion}
            options={getHelmChartVersionOptions(detail.data, chart)}
          />
          <Button
            disabled={!version || !(detail.data?.repositoryUrl || chart.repositoryUrl)}
            onClick={() =>
              onSelect({
                repositoryUrl: detail.data?.repositoryUrl || chart.repositoryUrl || '',
                chart: chart.name,
                version,
                values: {},
              })
            }
          >
            采用所选 Chart
          </Button>
          <Typography.Paragraph>
            {detail.data?.description || chart.description}
          </Typography.Paragraph>
          {detail.data?.readme ? (
            <Collapse
              items={[
                {
                  key: 'readme',
                  label: 'README',
                  children: (
                    <Typography.Paragraph
                      style={{ whiteSpace: 'pre-wrap', maxHeight: 360, overflow: 'auto' }}
                    >
                      {detail.data.readme}
                    </Typography.Paragraph>
                  ),
                },
              ]}
            />
          ) : null}
        </>
      ) : null}
    </Space>
  )
}

export function HelmEnvironmentFields({
  name,
  applicationId,
  environmentId,
  source,
}: {
  name: (string | number)[]
  applicationId: string
  environmentId: string
  source?: DeploymentTemplateHelmSource
}) {
  const sourceKey = JSON.stringify([applicationId, environmentId, source])
  const inspect = useMutation({
    mutationFn: async () => {
      if (!source) throw new Error('请选择包含 Chart 来源的模板')
      const data = await deliveryApi.applications.inspectHelmChart(applicationId, {
        applicationEnvironmentId: environmentId,
        source: parseHelmSource(source),
      })
      return { sourceKey, data }
    },
  })
  const inspection = inspect.data?.sourceKey === sourceKey ? inspect.data.data : undefined
  return (
    <>
      <Form.Item
        name={[...name, 'releaseName']}
        label="Release 名称"
        rules={[
          { required: true },
          {
            pattern: /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/,
            max: 53,
            message: '使用小写字母、数字或连字符，最多 53 字符',
          },
        ]}
      >
        <Input />
      </Form.Item>
      <HelmValuesField
        name={[...name, 'values']}
        label="环境 Values 覆盖"
        tooltip="与 Chart 默认值及模板 Values 合并，数组整体替换。"
        schema={inspection?.valuesSchema}
      />
      <Form.Item name={[...name, 'timeoutSeconds']} label="Helm 超时（秒）">
        <InputNumber min={1} max={3600} precision={0} />
      </Form.Item>
      <Form.List name={[...name, 'imageMappings']}>
        {(fields, { add, remove }) => (
          <Space orientation="vertical" style={{ width: '100%' }}>
            {fields.map((field) => (
              <Space key={field.key} wrap align="start">
                <Form.Item
                  name={[field.name, 'containerName']}
                  label="服务容器"
                  rules={[{ required: true }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item
                  name={[field.name, 'path']}
                  label="Values 路径"
                  tooltip="JSON Pointer，如 /image/repository 或 /containers/0/image。"
                  rules={[{ required: true, pattern: /^\// }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item
                  name={[field.name, 'value']}
                  label="镜像字段"
                  rules={[{ required: true }]}
                >
                  <Select
                    style={{ minWidth: 120 }}
                    options={[
                      { value: 'image', label: '完整镜像' },
                      { value: 'repository', label: '仓库' },
                      { value: 'digest', label: 'Digest' },
                    ]}
                  />
                </Form.Item>
                <Button onClick={() => remove(field.name)}>移除映射</Button>
              </Space>
            ))}
            <Button onClick={() => add({ value: 'image' })} disabled={fields.length >= 64}>
              添加镜像映射
            </Button>
          </Space>
        )}
      </Form.List>
      <Button loading={inspect.isPending} onClick={() => inspect.mutate()}>
        读取 Chart 默认值与 Schema
      </Button>
      {inspect.error ? <Alert type="error" showIcon title={inspect.error.message} /> : null}
      {inspection ? (
        <Collapse
          items={[
            {
              key: 'defaults',
              label: `${inspection.name} ${inspection.version} · 默认 Values`,
              children: (
                <Input.TextArea
                  aria-label="Chart 默认 Values"
                  readOnly
                  value={stringify(inspection.defaultValues)}
                  autoSize={{ minRows: 4, maxRows: 16 }}
                />
              ),
            },
            {
              key: 'schema',
              label: 'Values Schema',
              children: inspection.hasValuesSchema ? (
                <Input.TextArea
                  aria-label="Chart Values Schema"
                  readOnly
                  value={JSON.stringify(inspection.valuesSchema, null, 2)}
                  autoSize={{ minRows: 4, maxRows: 16 }}
                />
              ) : (
                <Typography.Text>此 Chart 未提供 Values Schema</Typography.Text>
              ),
            },
          ]}
        />
      ) : null}
    </>
  )
}

export function parseHelmConfiguration(
  config: HelmDeliveryConfiguration,
): HelmDeliveryConfiguration {
  return {
    ...config,
    source: parseHelmSource(config.source),
    values: parseHelmValues(config.values),
  }
}
