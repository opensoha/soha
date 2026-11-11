import { useState } from 'react'
import type { PersonalAssistant } from '../types'

export function AssistantAvatar({
  assistant,
  personal = true,
  busy = false,
  className = '',
}: {
  personal?: boolean
  assistant?: PersonalAssistant
  busy?: boolean
  className?: string
}) {
  const [failedSource, setFailedSource] = useState('')
  const fallback = personal ? '/heya-avatar.svg' : '/logo.svg'
  const image = assistant?.avatarUrl || fallback
  const source = failedSource === image ? fallback : image
  const poster = failedSource === image ? fallback : assistant?.avatarPosterUrl || source
  const off = assistant?.avatarMotion === 'off'
  return (
    <picture
      className={`soha-ai-avatar-image ${source === fallback ? 'is-default' : ''} ${busy && !off ? 'is-busy' : ''} ${className}`}
    >
      <source media="(prefers-reduced-motion: reduce)" srcSet={poster} />
      <img src={off ? poster : source} alt="" onError={() => setFailedSource(image)} />
    </picture>
  )
}
