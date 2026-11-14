/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { WorkbenchMarkdown } from './markdown'

describe('WorkbenchMarkdown', () => {
  it('opens only known citations and renders code and untrusted HTML safely', async () => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    const onSource = vi.fn()
    const source = { id: 'ref:abc', kind: 'document' as const, title: '维护规程', summary: '04:26' }
    const knowledgeSource = { ...source, id: 'citation:chunk-1', title: '知识来源' }
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    try {
      await act(async () => {
        root.render(
          <WorkbenchMarkdown
            content={
              '# 检索结果\n\n维护窗口 [citation:ref:abc]\n\n【citation:chunk-1】 【citation:missing】\n\n`[citation:ref:abc]`\n\n<script>alert(1)</script>\n\n![tracking](https://example.test/pixel)\n\n[bad](javascript:alert%281%29)'
            }
            sources={[source, knowledgeSource]}
            onSource={onSource}
          />,
        )
      })
      expect(container.querySelector('h1')?.textContent).toBe('检索结果')
      const citation = container.querySelector<HTMLButtonElement>(
        'button[aria-label="查看来源：维护规程"]',
      )
      expect(citation).not.toBeNull()
      await act(async () => {
        citation?.click()
      })
      expect(onSource).toHaveBeenCalledWith(source)
      expect(container.querySelectorAll('button')).toHaveLength(2)
      expect(container.querySelector('button[aria-label="查看来源：知识来源"]')).not.toBeNull()
      expect(container.textContent).toContain('[来源未验证]')
      expect(container.querySelector('code')?.textContent).toBe('[citation:ref:abc]')
      expect(container.querySelector('script, img, iframe')).toBeNull()
      expect(container.querySelector('a[href^="javascript:"]')).toBeNull()
    } finally {
      await act(async () => root.unmount())
      container.remove()
    }
  })
})
