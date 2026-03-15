package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/session"
)

func replayCmd() *cobra.Command {
	var replayEventLog string
	var format string
	var summaryOnly bool
	var follow bool
	var sinceBeginning bool
	var clearOnUpdate bool

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Replay a structured event log as a plain-text transcript",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if replayEventLog == "" {
				return fmt.Errorf("--event-log is required")
			}
			return runReplay(cmd.Context(), replayEventLog, format, summaryOnly, follow, sinceBeginning, clearOnUpdate, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&replayEventLog, "event-log", "", "path to a structured session event log (JSONL)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	cmd.Flags().BoolVar(&summaryOnly, "summary-only", false, "render only a concise summary instead of the full replay")
	cmd.Flags().BoolVar(&follow, "follow", false, "follow an append-only event log and stream replay output")
	cmd.Flags().BoolVar(&sinceBeginning, "since-beginning", true, "when following, replay existing events before streaming new ones")
	cmd.Flags().BoolVar(&clearOnUpdate, "clear-on-update", false, "when following, clear the screen before each rendered update")
	return cmd
}

func runReplay(ctx context.Context, eventLogPath, format string, summaryOnly, follow, sinceBeginning, clearOnUpdate bool, out io.Writer) error {
	if follow {
		return followReplay(ctx, eventLogPath, format, summaryOnly, sinceBeginning, clearOnUpdate, out)
	}
	f, err := os.Open(eventLogPath)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	events, err := session.DecodeJSONLEvents(f)
	if err != nil {
		return fmt.Errorf("decode event log: %w", err)
	}
	return renderReplay(events, format, summaryOnly, out)
}

func renderReplay(events []session.Event, format string, summaryOnly bool, out io.Writer) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		var content string
		if summaryOnly {
			content = session.BuildSummaryText(session.BuildSummary(events))
		} else {
			content = session.BuildTranscript(events)
		}
		if content == "" {
			return nil
		}
		if _, err := fmt.Fprintln(out, content); err != nil {
			return fmt.Errorf("write transcript: %w", err)
		}
		return nil
	case "json":
		var payload []byte
		var err error
		if summaryOnly {
			payload, err = session.MarshalSummaryJSON(session.BuildSummary(events))
		} else {
			payload, err = session.MarshalEventsJSON(events)
		}
		if err != nil {
			return fmt.Errorf("marshal replay output: %w", err)
		}
		if len(payload) == 0 {
			return nil
		}
		if _, err := fmt.Fprintln(out, string(payload)); err != nil {
			return fmt.Errorf("write replay output: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported replay format %q", format)
	}
}

func followReplay(ctx context.Context, eventLogPath, format string, summaryOnly, sinceBeginning, clearOnUpdate bool, out io.Writer) error {
	f, err := os.Open(eventLogPath)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	if !sinceBeginning {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("seek event log end: %w", err)
		}
	}

	reader := bufio.NewReader(f)
	var events []session.Event
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return fmt.Errorf("follow event log: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var evt session.Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			return fmt.Errorf("decode followed event: %w", err)
		}
		events = append(events, evt)
		if err := renderReplayFollowChunk(events, evt, format, summaryOnly, clearOnUpdate, out); err != nil {
			return err
		}
	}
}

func renderReplayFollowChunk(events []session.Event, evt session.Event, format string, summaryOnly, clearOnUpdate bool, out io.Writer) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "text":
		var content string
		if summaryOnly {
			content = session.BuildSummaryText(session.BuildSummary(events))
			if content != "" {
				content = "---\n" + content
			}
		} else {
			content = session.BuildTranscriptEvent(evt)
		}
		if strings.TrimSpace(content) == "" {
			return nil
		}
		if clearOnUpdate {
			if _, err := io.WriteString(out, "\x1b[H\x1b[2J"); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(out, content)
		return err
	case "json":
		var payload []byte
		var err error
		if summaryOnly {
			payload, err = session.MarshalSummaryJSON(session.BuildSummary(events))
		} else {
			payload, err = json.Marshal(evt)
		}
		if err != nil {
			return fmt.Errorf("marshal followed replay output: %w", err)
		}
		if len(payload) == 0 {
			return nil
		}
		if clearOnUpdate {
			if _, err := io.WriteString(out, "\x1b[H\x1b[2J"); err != nil {
				return err
			}
		}
		_, err = fmt.Fprintln(out, string(payload))
		return err
	default:
		return fmt.Errorf("unsupported replay format %q", format)
	}
}
