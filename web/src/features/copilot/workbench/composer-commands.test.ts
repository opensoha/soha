import { describe, expect, it } from 'vitest'
import { matchingCommands, replaceSlashToken, slashToken } from './composer-commands'

describe('chat slash commands', () => {
  it('matches partial names and Chinese labels at the end of a draft', () => {
    expect(matchingCommands('/pla').map((item) => item.value)).toEqual(['plan'])
    expect(matchingCommands('/analysis').map((item) => item.value)).toEqual(['analysis'])
    expect(matchingCommands('保留这段草稿 /上下文').map((item) => item.value)).toEqual(['context'])
    expect(replaceSlashToken('保留这段草稿 /pla', '新内容')).toBe('保留这段草稿 新内容')
    expect(replaceSlashToken('保留这段草稿 /tools', '')).toBe('保留这段草稿 ')
  })

  it('leaves URLs, paths and unmatched commands as ordinary text', () => {
    for (const value of ['https://example.com/plan', '/var/log/app.log', '文本/resources']) {
      expect(slashToken(value)).toBeUndefined()
      expect(replaceSlashToken(value, 'replacement')).toBe(value)
    }
    expect(matchingCommands('/unknown')).toEqual([])
  })
})
