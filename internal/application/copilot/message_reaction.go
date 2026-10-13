package copilot

import (
	"slices"
	"strings"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
)

var messageReactions = []string{"👋", "👍", "❤️", "😊", "🎉", "💡", "👀", "🤔", "✅", "🙏"}

const reactionInstruction = "You may optionally acknowledge the latest user message with ONE emoji from 👋 👍 ❤️ 😊 🎉 💡 👀 🤔 ✅ 🙏. When natural, start your reply with [[reaction:EMOJI]] on its own line, then write the normal reply. This is a social reaction, not proof of execution or approval. Omit it when inappropriate; never invent another marker or react to historical messages."

func splitMessageReaction(content string) (string, string) {
	if rest, ok := strings.CutPrefix(content, "[[reaction:"); ok {
		if emoji, body, closed := strings.Cut(rest, "]]"); closed && slices.Contains(messageReactions, emoji) {
			return strings.TrimLeft(body, "\r\n"), emoji
		}
	}
	return content, ""
}

func (cp *nativeCheckpoint) acceptReaction(response *appaigateway.WorkbenchRelayResponse) {
	content, reaction := splitMessageReaction(response.Content)
	response.Content = content
	if reaction != "" {
		cp.Reaction = reaction
	}
}

// ponytail: one bounded leading marker works across providers without another model call.
// Replace with typed response metadata only when all supported protocols share that facility.
type reactionStream struct {
	buffer   string
	decided  bool
	trimLine bool
	send     func(string) bool
}

func (s *reactionStream) push(delta string) bool {
	if s.decided {
		return s.emit(delta)
	}
	s.buffer += delta
	const prefix = "[[reaction:"
	if strings.HasPrefix(prefix, s.buffer) {
		return true
	}
	if strings.HasPrefix(s.buffer, prefix) && !strings.Contains(s.buffer, "]]") && len(s.buffer) <= 64 {
		return true
	}
	return s.flush()
}

func (s *reactionStream) flush() bool {
	s.decided = true
	content, emoji := splitMessageReaction(s.buffer)
	if emoji != "" {
		s.trimLine = true
	}
	s.buffer = ""
	return s.emit(content)
}

func (s *reactionStream) emit(content string) bool {
	if s.trimLine {
		content = strings.TrimLeft(content, "\r\n")
		s.trimLine = content == ""
	}
	return content == "" || s.send(content)
}

func replyReactionMetadata(metadata map[string]any, reply chatReply, requestMessageID string) {
	if reply.Reaction != "" && !chatReplyFailed(reply) {
		metadata["assistantReaction"] = reply.Reaction
		metadata["requestMessageId"] = requestMessageID
	}
}
