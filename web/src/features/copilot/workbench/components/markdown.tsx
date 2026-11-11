import { SearchHighlight } from './message-search'
import {
  Children,
  cloneElement,
  isValidElement,
  createContext,
  useContext,
  useMemo,
  type ReactNode,
  type CSSProperties,
} from 'react'
import { theme } from 'antd'
import { XMarkdown, type ComponentProps, type XMarkdownProps } from '@ant-design/x-markdown'
import type { WorkbenchSource } from '@opensoha/contracts/gen/ts/sohaapi'
import '@ant-design/x-markdown/themes/light.css'

const noSources: WorkbenchSource[] = []
const SourceContext = createContext<{
  sources: WorkbenchSource[]
  onSource?: (source: WorkbenchSource) => void
}>({ sources: noSources })

const citationConfig: XMarkdownProps['config'] = {
  extensions: [
    {
      name: 'sohaCitation',
      level: 'inline',
      start: (source) => source.search(/(?:\[|【)citation:/),
      tokenizer(source) {
        const match = /^(?:\[|【)citation:([a-zA-Z0-9:_-]{1,180})(?:\]|】)/.exec(source)
        if (match) return { type: 'sohaCitation', raw: match[0], citationId: match[1] }
      },
      renderer: (token) =>
        `<a href="#soha-citation:${encodeURIComponent(String(token.citationId))}">[来源]</a>`,
    },
  ],
}

export function safeSourceURL(value: unknown) {
  if (typeof value !== 'string') return undefined
  try {
    const url = new URL(value)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password
      ? url.href
      : undefined
  } catch {
    return undefined
  }
}

function MarkdownLink({ href, children }: ComponentProps<{ href?: string }>) {
  const { sources, onSource } = useContext(SourceContext)
  if (href?.startsWith('#soha-citation:')) {
    const id = href.slice('#soha-citation:'.length)
    const index = sources.findIndex(
      (source) =>
        encodeURIComponent(source.id) === id ||
        encodeURIComponent(source.id) === `citation%3A${id}`,
    )
    const source = sources[index]
    return source && onSource ? (
      <button
        type="button"
        className="soha-ai-citation"
        aria-label={`查看来源：${source.title}`}
        onClick={() => onSource(source)}
      >
        [{index + 1}]
      </button>
    ) : (
      <span title="未找到对应来源">[来源未验证]</span>
    )
  }
  const url = safeSourceURL(href)
  return url ? (
    <a href={url} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  ) : (
    <span>{children}</span>
  )
}

function MarkdownImage({ alt }: ComponentProps<{ alt?: string }>) {
  return <span>{alt ? `图片：${alt}` : '图片链接'}</span>
}

const SearchContext = createContext('')
function highlightChildren(children: ReactNode, query: string): ReactNode {
  return Children.map(children, (child) =>
    typeof child === 'string' ? (
      <SearchHighlight text={child} query={query} />
    ) : isValidElement<{ children?: ReactNode }>(child) ? (
      cloneElement(child, {}, highlightChildren(child.props.children, query))
    ) : (
      child
    ),
  )
}
function SearchParagraph({ children }: ComponentProps) {
  const query = useContext(SearchContext)
  return <p>{query ? highlightChildren(children, query) : children}</p>
}
function SearchListItem({ children }: ComponentProps) {
  const query = useContext(SearchContext)
  return <li>{query ? highlightChildren(children, query) : children}</li>
}
const markdownComponents = {
  a: MarkdownLink,
  img: MarkdownImage,
  p: SearchParagraph,
  li: SearchListItem,
}

export function WorkbenchMarkdown({
  content,
  streaming = false,
  searchQuery = '',
  sources = noSources,
  onSource,
}: {
  content: string
  streaming?: boolean
  searchQuery?: string
  sources?: WorkbenchSource[]
  onSource?: (source: WorkbenchSource) => void
}) {
  const { token } = theme.useToken()
  const context = useMemo(() => ({ sources, onSource }), [sources, onSource])
  return (
    <SourceContext.Provider value={context}>
      <SearchContext.Provider value={searchQuery}>
        <XMarkdown
          content={content}
          className="x-markdown-light soha-ai-markdown"
          escapeRawHtml
          config={citationConfig}
          components={markdownComponents}
          streaming={{ hasNextChunk: streaming }}
          style={
            {
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
      </SearchContext.Provider>
    </SourceContext.Provider>
  )
}
