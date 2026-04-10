package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/julianshen/rubichan/internal/store"
)

func sessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage sessions",
	}
	cmd.AddCommand(sessionListCmd())
	cmd.AddCommand(sessionForkCmd())
	cmd.AddCommand(sessionDeleteCmd())
	return cmd
}

func sessionListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent sessions",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := openSessionStore()
			if err != nil {
				return err
			}
			defer s.Close()

			// Fetch more than we display so working-dir filter doesn't hide results
			sessions, err := s.ListSessions(200)
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()
			shown := 0
			for _, sess := range sessions {
				if !all && sess.WorkingDir != cwd {
					continue
				}
				if shown >= 20 {
					break
				}
				shown++
				title := sess.Title
				if title == "" {
					title = "(untitled)"
				}
				if sess.ForkedFrom != "" {
					title += fmt.Sprintf(" (forked from %s)", truncateID(sess.ForkedFrom))
				}
				ago := time.Since(sess.UpdatedAt).Truncate(time.Minute)
				fmt.Printf("%-36s  %-35s  %-15s  %s ago\n", sess.ID, title, sess.Model, ago)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "show sessions from all directories")
	return cmd
}

func sessionForkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fork <session-id>",
		Short: "Fork a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, err := openSessionStore()
			if err != nil {
				return err
			}
			defer s.Close()

			sourceID := args[0]
			newID := uuid.New().String()
			if err := s.ForkSession(sourceID, newID); err != nil {
				return err
			}
			fmt.Printf("Forked session %s → %s\nResume with: rubichan --resume %s\n", truncateID(sourceID), truncateID(newID), newID)
			return nil
		},
	}
}

func sessionDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			s, err := openSessionStore()
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.DeleteSession(args[0]); err != nil {
				return err
			}
			fmt.Printf("Deleted session %s\n", args[0])
			return nil
		},
	}
}

func openSessionStore() (*store.Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(home, ".config", "rubichan", "rubichan.db")
	return store.NewStore(dbPath)
}

func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
