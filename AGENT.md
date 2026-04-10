# AGENT.md

This file documents Rubichan's session persistence and resume features for end users.

## Resume Session

Rubichan automatically saves your interactive sessions and allows you to resume where you left off.

### Starting Rubichan

When you start Rubichan in interactive mode without specifying a session, it checks for previous sessions:

```bash
rubichan interactive
```

If previous sessions exist, Rubichan displays the session selector:

```
📋 Resume Session

→ [sess-001] just now (5 turns)
  [sess-002] 2 hours ago (12 turns)
  [sess-003] Jan 2, 15:04 (3 turns)

Use ↑↓ to navigate, Enter to resume, Esc to cancel
```

Use the arrow keys or `j`/`k` (vim keys) to navigate. Press Enter to resume the selected session, or Esc/`q` to cancel and start a new session.

### Resume by Session ID

To resume a specific session directly without the selector:

```bash
rubichan interactive --resume sess-123
```

Or using the short flag:

```bash
rubichan -r sess-123
```

### Session Information

Each session is identified by:
- **ID**: Unique session identifier (e.g., `sess-001`)
- **Time**: Relative time since creation (e.g., "2 hours ago", "Jan 2, 15:04")
- **Turns**: Number of completed exchanges (each turn = one user input + one agent response)

### Session Storage

Sessions are stored in SQLite at:

```
~/.config/rubichan/skills.db
```

The database contains:
- Session metadata (ID, creation time, model used, working directory)
- All messages (user inputs and agent responses)
- Message metadata (timestamps, content blocks)

### Managing Sessions

Sessions are never automatically deleted. To list sessions on the command line:

```bash
rubichan session list
```

To remove a specific session:

```bash
rubichan session delete sess-123
```

Or to delete a session and keep a copy for reference:

```bash
rubichan session fork --resume sess-123
```

This creates a new independent session forked from the original.

### Session Context

When you resume a session:
- All previous messages (user inputs and agent responses) are restored
- The working directory, model, and system configuration are preserved
- You can continue the conversation exactly where you left off
- Token usage is tracked from the session start time

Each resumed session maintains its own context and does not affect other sessions.
