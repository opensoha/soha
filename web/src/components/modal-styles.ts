import type { ModalProps } from 'antd'
import type { CSSProperties } from 'react'

export const viewportModalStyle: CSSProperties = {
  top: 16,
  paddingBottom: 0,
}

export const scrollableModalBodyStyle: CSSProperties = {
  maxHeight: 'calc(100dvh - 96px)',
  overflowY: 'auto',
  overscrollBehavior: 'contain',
  scrollbarGutter: 'stable',
}

export const hiddenModalHeaderStyle: CSSProperties = {
  minHeight: 32,
}

export const visuallyHiddenModalTitleStyle: CSSProperties = {
  position: 'absolute',
  width: 1,
  height: 1,
  padding: 0,
  margin: -1,
  overflow: 'hidden',
  clip: 'rect(0, 0, 0, 0)',
  whiteSpace: 'nowrap',
  border: 0,
}

export const operationModalStyles: ModalProps['styles'] = {
  container: { display: 'flex', flexDirection: 'column', maxHeight: 'calc(100dvh - 64px)' },
  body: { minHeight: 0, overflowY: 'auto', overscrollBehavior: 'contain' },
  footer: { flexShrink: 0 },
}
