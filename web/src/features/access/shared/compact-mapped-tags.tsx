import { Button, Popover, Space } from 'antd'
import { MetadataTag, type MetadataTagTone } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { TableCellText } from '@/components/table-cell-content'

interface CompactMappedTagsProps {
  emptyText: string
  itemLabel: string
  labelMap: Record<string, string>
  values: string[]
  visibleCount: number
  tone: MetadataTagTone
}

function CompactMappedTags({
  emptyText,
  itemLabel,
  labelMap,
  values,
  visibleCount,
  tone,
}: CompactMappedTagsProps) {
  const { localeCode } = useI18n()
  if (!values?.length) return emptyText

  const visibleValues = values.slice(0, visibleCount)
  const hiddenCount = Math.max(values.length - visibleValues.length, 0)
  const renderTag = (value: string, className = 'soha-access-compact-tag') => {
    const label = labelMap[value] || value
    return (
      <MetadataTag
        key={value}
        className={className}
        tone={tone}
        label={<TableCellText className="soha-access-compact-tag-text" value={label} />}
      />
    )
  }
  if (hiddenCount === 0) {
    return (
      <Space wrap={false} size={4} className="soha-access-compact-tags">
        {visibleValues.map((value) => renderTag(value))}
      </Space>
    )
  }

  return (
    <Popover
      trigger="click"
      destroyOnHidden
      placement="topLeft"
      title={
        localeCode === 'zh_CN' ? `${values.length} 个${itemLabel}` : `${values.length} ${itemLabel}`
      }
      content={
        <div className="soha-access-permission-popover">
          {values.map((value) => renderTag(value, 'soha-access-permission-popover-tag'))}
        </div>
      }
    >
      <Button
        type="text"
        size="small"
        className="soha-access-compact-tags-trigger"
        aria-label={
          localeCode === 'zh_CN'
            ? `查看 ${values.length} 个${itemLabel}`
            : `View ${values.length} ${itemLabel}`
        }
        onClick={(event) => {
          event.preventDefault()
          event.stopPropagation()
        }}
      >
        <Space wrap={false} size={4} className="soha-access-compact-tags">
          {visibleValues.map((value) => renderTag(value))}
          <MetadataTag className="soha-access-compact-tag-more" label={`+${hiddenCount}`} />
        </Space>
      </Button>
    </Popover>
  )
}

export function renderCompactMappedTags(
  values: string[],
  labelMap: Record<string, string>,
  emptyText = '-',
  visibleCount = 2,
  itemLabel = '权限项',
  tone: MetadataTagTone = 'blue',
) {
  return (
    <CompactMappedTags
      emptyText={emptyText}
      itemLabel={itemLabel}
      labelMap={labelMap}
      values={values}
      visibleCount={visibleCount}
      tone={tone}
    />
  )
}
