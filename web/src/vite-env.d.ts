/// <reference types="vite/client" />

declare module 'monaco-editor/esm/vs/basic-languages/yaml/yaml' {
  import type * as Monaco from 'monaco-editor'

  export const conf: Monaco.languages.LanguageConfiguration
  export const language: Monaco.languages.IMonarchLanguage
}
