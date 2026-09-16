package copilot

func chatReplyFailed(reply chatReply) bool {
	switch reply.Source {
	case "model-unconfigured", "model-error", "model-empty":
		return true
	default:
		return false
	}
}

func safeChatReply(reply chatReply, locale string) chatReply {
	if chatReplyFailed(reply) {
		reply.Content = localize(locale, "模型暂不可用，请检查模型配置或稍后重试。", "The model is unavailable. Check its configuration or try again later.")
		reply.Error = ""
	}
	return reply
}

func chatReplyRole(reply chatReply) string {
	if chatReplyFailed(reply) {
		return "system"
	}
	return "assistant"
}
