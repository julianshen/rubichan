package agent

import "context"

type reactiveResult struct {
	compacted bool
}

func reactiveCompact(ctx context.Context, cm *ContextManager, conv *Conversation) reactiveResult {
	if conv.Len() == 0 {
		return reactiveResult{}
	}
	result := cm.ForceCompact(ctx, conv)
	if result.AfterMsgCount < result.BeforeMsgCount && result.AfterMsgCount > 0 {
		return reactiveResult{compacted: true}
	}
	return reactiveResult{}
}

func contextCollapseDrain[T any](messages []T, minPairsToKeep int) []T {
	if len(messages) <= minPairsToKeep*2 {
		return messages
	}
	pairsToRemove := (len(messages) - minPairsToKeep*2) / 2
	if pairsToRemove <= 0 {
		return messages
	}
	cutoff := pairsToRemove * 2
	if cutoff >= len(messages) {
		return messages
	}
	return messages[cutoff:]
}
