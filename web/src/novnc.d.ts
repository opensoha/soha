declare module '@novnc/novnc' {
  export default class RFB {
    constructor(
      target: HTMLElement,
      url: string,
      options?: {
        credentials?: { password?: string }
        repeaterID?: string
        shared?: boolean
        wsProtocols?: string[]
      }
    )

    scaleViewport: boolean
    resizeSession: boolean
    viewOnly: boolean
    clipViewport: boolean
    dragViewport: boolean
    showDotCursor: boolean
    background: string
    qualityLevel: number
    compressionLevel: number

    disconnect(): void
    sendCredentials(creds: { username?: string; password?: string; target?: string }): void
    sendKey(keysym: number, code: string, down?: boolean): void
    sendCtrlAltDel(): void
    focus(): void
    blur(): void
    machineShutdown(): void
    machineReboot(): void
    machineReset(): void
    clipboardPasteFrom(text: string): void

    addEventListener(type: string, listener: (e: any) => void): void
    removeEventListener(type: string, listener: (e: any) => void): void
  }
}
