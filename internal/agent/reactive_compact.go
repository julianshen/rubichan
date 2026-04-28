package agent

import "context"

func reactiveCompact(ctx context.Context, cm *ContextManager, conv *Conversation) bool {
	if conv.Len() == 0 {
		return false
	}
	result := cm.ForceCompact(ctx, conv)
	return result.AfterMsgCount < result.BeforeMsgCount && result.AfterMsgCount > 0
}

const minDrainPairs = 2
