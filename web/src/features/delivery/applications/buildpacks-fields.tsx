import { useEffect } from 'react'
import { Alert, Button, Form, Input, type FormInstance } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { deliveryQueries } from '../queries'

export function BuildpacksFields({
  applicationId,
  form,
  prefix,
}: {
  applicationId?: string
  form: FormInstance
  prefix: (string | number)[]
}) {
  const capability = useQuery(
    deliveryQueries.applications.buildpacksCapability(applicationId ?? ''),
  )
  const pathKey = JSON.stringify([...prefix, 'config', 'buildpacks'])
  const field = (...path: string[]) => [...prefix, 'config', ...path]
  useEffect(() => {
    const path = JSON.parse(pathKey) as (string | number)[]
    if (
      capability.data?.ready &&
      capability.data.configuration &&
      !form.getFieldValue(path)?.builderImage
    ) {
      form.setFieldValue(path, { ...form.getFieldValue(path), ...capability.data.configuration })
    }
  }, [capability.data, form, pathKey])
  return (
    <>
      {!capability.data?.ready ? (
        <Alert
          type={capability.isError ? 'error' : 'warning'}
          showIcon
          title={
            !applicationId
              ? '先保存应用以检查 Buildpacks 能力'
              : capability.isFetching
                ? '正在检查 Buildpacks 执行器'
                : 'Buildpacks 暂不可执行'
          }
          description={capability.error?.message || capability.data?.reason}
          action={
            applicationId ? (
              <Button size="small" onClick={() => void capability.refetch()}>
                重新检查
              </Button>
            ) : undefined
          }
        />
      ) : null}
      {(['builderImage', 'runImage', 'platform'] as const).map((name, index) => (
        <Form.Item
          key={name}
          name={field('buildpacks', name)}
          label={['Builder 镜像', '运行基础镜像', '构建架构'][index]}
          rules={[{ required: true, message: '需要平台提供已就绪的 Buildpacks 配置' }]}
        >
          <Input readOnly />
        </Form.Item>
      ))}
      <Form.Item label="平台配置">
        <Button
          disabled={!capability.data?.ready || !capability.data.configuration}
          onClick={() => {
            form.setFieldValue(field('buildpacks'), {
              ...form.getFieldValue(field('buildpacks')),
              ...capability.data?.configuration,
            })
          }}
        >
          使用当前平台配置
        </Button>
      </Form.Item>
      <Form.Item
        name={field('contextDir')}
        label="项目目录"
        tooltip="相对于源码仓库根目录；每次构建使用一个仓库，不拉取子模块。"
      >
        <Input placeholder="." />
      </Form.Item>
      <Form.Item
        name={field('buildpacks', 'processType')}
        label="启动进程"
        tooltip="留空使用 Buildpacks 检测出的默认进程，例如 web。"
        rules={[
          { pattern: /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/, max: 64, message: '请输入有效的进程名称' },
        ]}
      >
        <Input placeholder="web" />
      </Form.Item>
      <Form.Item
        name={field('variables')}
        label="构建变量"
        tooltip={'字符串 JSON 对象，例如 {"BP_JVM_VERSION":"21"}；凭据填写下方密钥引用。'}
        getValueProps={(value: unknown) => ({
          value: typeof value === 'string' ? value : JSON.stringify(value ?? {}, null, 2),
        })}
        normalize={(value: string) => {
          try {
            return JSON.parse(value || '{}')
          } catch {
            return value
          }
        }}
        rules={[
          {
            validator: async (_, value: unknown) => {
              if (value === undefined) return
              if (
                !value ||
                typeof value !== 'object' ||
                Array.isArray(value) ||
                Object.entries(value).some(
                  ([key, item]) =>
                    !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || typeof item !== 'string',
                )
              ) {
                throw new Error('请输入变量名有效、值为字符串的 JSON 对象')
              }
            },
          },
        ]}
      >
        <Input.TextArea rows={3} />
      </Form.Item>
      {(
        [
          ['GIT_USERNAME', 'Git 用户名密钥'],
          ['GIT_PASSWORD', 'Git 密码密钥'],
          ['GIT_SSH_KEY', 'Git SSH 私钥'],
          ['GIT_KNOWN_HOSTS', 'Git SSH 主机指纹'],
          ['REGISTRY_AUTH', '镜像仓库认证密钥'],
        ] as const
      ).map(([key, label]) => (
        <Form.Item
          key={key}
          name={field('secretRefs', key)}
          label={label}
          tooltip="填写有权使用的 Soha 密钥引用。SSH 需私钥与 known_hosts；子模块须绑定仓库、路径和固定提交。镜像仓库认证值为 Docker auth JSON。"
          normalize={(value: string) => value.trim() || undefined}
          rules={[
            {
              pattern: /^soha:\/\/secrets\/[^/\s]+(?:\/versions\/\d+)?$/,
              message: '请输入 Soha 密钥引用',
            },
          ]}
        >
          <Input placeholder="soha://secrets/…" autoComplete="off" />
        </Form.Item>
      ))}
    </>
  )
}
