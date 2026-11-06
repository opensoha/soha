import { XMarkdown, type ComponentProps } from '@ant-design/x-markdown'
import { theme, Typography } from 'antd'
import type { CSSProperties } from 'react'
import { useI18n } from '@/i18n'
import '@ant-design/x-markdown/themes/light.css'

function safeLink(value?: string) {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

function ReadmeLink({ href, children }: ComponentProps<{ href?: string }>) {
  const url = safeLink(href)
  return url ? (
    <a href={url} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  ) : (
    <span>{children}</span>
  )
}

function ReadmeImage({ src, alt }: ComponentProps<{ src?: string; alt?: string }>) {
  const url = safeLink(src)
  const { localeCode } = useI18n()
  const label = alt || (localeCode === 'zh_CN' ? '查看图片' : 'View image')
  return url ? (
    <a href={url} target="_blank" rel="noopener noreferrer">
      {label}
    </a>
  ) : (
    <span>{label}</span>
  )
}

const readmeComponents = { a: ReadmeLink, img: ReadmeImage }

export function ChartReadme({ value }: { value?: string }) {
  const { token } = theme.useToken()
  const { localeCode } = useI18n()
  if (!value?.trim()) {
    return (
      <Typography.Text type="secondary">
        {localeCode === 'zh_CN' ? '此版本未提供 README' : 'No README provided for this version'}
      </Typography.Text>
    )
  }

  return (
    <XMarkdown
      content={value}
      className="x-markdown-light soha-helm-chart-readme"
      escapeRawHtml
      components={readmeComponents}
      streaming={{ hasNextChunk: false }}
      style={
        {
          overflowWrap: 'anywhere',
          minWidth: 0,
          '--text-color': token.colorText,
          '--heading-color': token.colorTextHeading,
          '--primary-color': token.colorLink,
          '--primary-color-hover': token.colorLinkHover,
          '--border-color': token.colorBorderSecondary,
          '--line-color': token.colorBorderSecondary,
          '--light-bg': token.colorFillTertiary,
          '--table-head-bg': token.colorFillAlter,
          '--table-body-bg': token.colorBgContainer,
          '--cite-bg': token.colorFillSecondary,
          '--cite-hover-bg': token.colorFill,
        } as CSSProperties
      }
    />
  )
}
