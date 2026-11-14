import type * as Monaco from 'monaco-editor'
import { conf, language } from 'monaco-editor/esm/vs/basic-languages/yaml/yaml'

let yamlTokensRegistered = false

export function ensureYamlLanguage(monaco: typeof Monaco) {
  if (!yamlTokensRegistered) {
    monaco.languages.setLanguageConfiguration('yaml', conf)
    monaco.languages.setMonarchTokensProvider('yaml', language)
    yamlTokensRegistered = true
  }
}
