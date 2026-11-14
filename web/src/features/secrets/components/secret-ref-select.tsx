import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import type { SelectProps } from 'antd'
import { Select } from 'antd'
import { buildSecretReference, isSecretReferenceId } from '../api'
import { secretQueries } from '../queries'
export function SecretRefSelect({
  disabled,
  enabled = true,
  value,
  ...props
}: SelectProps<string> & {
  enabled?: boolean
}) {
  const secretsQuery = useQuery(secretQueries.list({}, enabled && !disabled))
  const visibleSecretsQueryData =
    secretsQuery.isEnabled === false ? undefined : readableQueryData(secretsQuery)
  const options = (visibleSecretsQueryData ?? [])
    .filter((secret) => secret.status === 'active' && isSecretReferenceId(secret.id))
    .map((secret) => ({
      label: `${secret.name} · ${secret.scopeType}/${secret.scopeId}`,
      value: buildSecretReference(secret.id),
    }))
  if (value && !options.some((option) => option.value === value)) {
    options.unshift({ label: value, value })
  }
  return (
    <>
      <ReadFeedback canRead={secretsQuery.isEnabled} query={secretsQuery} label="Secret目录" />
      <Select
        {...props}
        allowClear
        disabled={disabled}
        loading={secretsQuery.isFetching}
        options={options}
        placeholder={props.placeholder ?? '选择 Secret'}
        showSearch={{ optionFilterProp: 'label' }}
        value={value}
      />
    </>
  )
}
