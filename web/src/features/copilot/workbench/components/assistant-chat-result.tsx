import { SearchHighlight } from './message-search'
import { Button, Image, Space } from 'antd'
import type { PersonalAssistantAvatarInput } from '../types'
import type { ConversationMessage } from '../conversation'

export function UserMessageContent({
  message,
  reaction,
  searchQuery,
}: {
  message: ConversationMessage
  reaction?: string
  searchQuery?: string
}) {
  const images = Array.isArray(message.metadata?.images) ? message.metadata.images : []
  return (
    <div className="soha-ai-user-message">
      {images.map((image: { id?: string; name?: string; dataUrl?: string }) =>
        typeof image?.dataUrl === 'string' &&
        image.dataUrl.length <= 700000 &&
        /^data:image\/(png|jpeg);base64,[A-Za-z0-9+/]+=*$/.test(image.dataUrl) ? (
          <Image
            key={image.id}
            src={image.dataUrl}
            alt={image.name || '聊天图片'}
            className="soha-ai-chat-image"
          />
        ) : null,
      )}
      <div>
        {searchQuery ? (
          <SearchHighlight text={message.content} query={searchQuery} />
        ) : (
          message.content
        )}
      </div>
      {reaction && (
        <span className="soha-ai-message-reaction" role="img" aria-label={`助手回应 ${reaction}`}>
          {reaction}
        </span>
      )}
    </div>
  )
}

export function AssistantChatResult({
  metadata,
  loading,
  readOnly = false,
  onContinue,
  onUndo,
}: {
  metadata?: Record<string, unknown>
  loading?: boolean
  readOnly?: boolean
  onContinue?: () => void
  onUndo?: () => void
}) {
  const image = metadata?.assistantImage as PersonalAssistantAvatarInput | undefined
  const source = image?.avatarPosterUrl || image?.avatarUrl
  const valid = source && /^data:image\/(png|jpeg|gif|webp);base64,/.test(source)
  const updated = metadata?.assistantProfileUpdated === true && !!metadata?.assistantProfileApplied
  if (!valid && !updated) return null
  return (
    <div className="soha-ai-assistant-chat-result">
      {valid && <img src={source} alt="助手形象" />}
      {updated && !readOnly && (
        <Space wrap>
          <Button size="small" type="text" disabled={loading} onClick={onContinue}>
            继续调整
          </Button>
          <Button size="small" type="text" disabled={loading} onClick={onUndo}>
            撤销这次更改
          </Button>
        </Space>
      )}
    </div>
  )
}
