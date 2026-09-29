package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Pratyush-who/WhatsDown/internal/whatsapp"
	"github.com/chzyer/readline"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "pstw",
		Short: "Personal WhatsApp terminal client",
		RunE:  runConnection,
	}
	root.AddCommand(
		setupCommand(),
		statusCommand(),
		syncCommand(),
		sendCommand(),
		mediaCommand(),
		scheduleCommand(),
		daemonCommand(),
		chatsCommand(),
		inspectCommand(),
		completionCommand(root),
	)
	return root
}

func setupCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Pair or reconnect your WhatsApp account",
		RunE:  runConnection,
	}
}

func statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Connect and show WhatsApp status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd, false, func(_ context.Context, client *whatsapp.Client) error { printStatus(client); return nil })
		},
	}
}

func getDataDir() string {
	if envDir := os.Getenv("WHATSDOWN_DATA_DIR"); envDir != "" {
		return envDir
	}
	if envDir := os.Getenv("PSTW_DATA_DIR"); envDir != "" {
		return envDir
	}

	// 1. If running in source repo or working directory with existing data folder containing DB
	if stat, err := os.Stat("data"); err == nil && stat.IsDir() {
		if _, err := os.Stat(filepath.Join("data", "whatsapp.db")); err == nil {
			if abs, err := filepath.Abs("data"); err == nil {
				return abs
			}
			return "data"
		}
	}

	// 2. Check next to the executable (portable installation with existing DB)
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		// Skip temp directory used by 'go run'
		if !strings.Contains(strings.ToLower(exeDir), "temp") {
			exeData := filepath.Join(exeDir, "data")
			if stat, err := os.Stat(exeData); err == nil && stat.IsDir() {
				if _, err := os.Stat(filepath.Join(exeData, "whatsapp.db")); err == nil {
					return exeData
				}
			}
		}
	}

	// 3. Persistent user config/data directory (e.g. %APPDATA%/whatsdown or ~/.config/whatsdown)
	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		return filepath.Join(configDir, "whatsdown")
	}

	if abs, err := filepath.Abs("data"); err == nil {
		return abs
	}
	return "data"
}

func withClient(cmd *cobra.Command, renderQR bool, action func(context.Context, *whatsapp.Client) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := whatsapp.New(ctx, getDataDir())
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Connect(ctx, renderQR); err != nil {
		return err
	}
	return action(ctx, client)
}

func sendCommand() *cobra.Command {
	var filePath string
	command := &cobra.Command{
		Use:   "send <recipient> [message]",
		Short: "Send a text message or file",
		Args:  cobra.MinimumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeContacts(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" && len(args) < 2 {
				return errors.New("message is required unless --file is used")
			}
			return withClient(cmd, true, func(ctx context.Context, client *whatsapp.Client) error {
				if filePath != "" {
					caption := ""
					if len(args) > 1 {
						caption = strings.Join(args[1:], " ")
					}
					if err := client.SendMedia(ctx, args[0], filePath, caption); err != nil {
						return err
					}
					fmt.Println("File sent.")
					return nil
				}
				recipient, text, err := splitRecipientMessage(ctx, client, args)
				if err != nil {
					return err
				}
				if err := client.SendText(ctx, recipient, text); err != nil {
					return err
				}
				fmt.Println("Message sent.")
				return nil
			})
		},
	}
	command.Flags().StringVar(&filePath, "file", "", "send a media/document from this path")
	return command
}

func mediaCommand() *cobra.Command {
	var caption string
	command := &cobra.Command{
		Use:   "media <recipient> <file-path> [caption]",
		Short: "Send an image, video, audio, or document",
		Args:  cobra.MinimumNArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeContacts(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			if len(args) == 1 {
				return nil, cobra.ShellCompDirectiveDefault
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, true, func(ctx context.Context, client *whatsapp.Client) error {
				recipient, filePath, parsedCaption, err := parseMediaArgs(ctx, client, args)
				if err != nil {
					return err
				}
				if caption != "" {
					parsedCaption = caption
				}
				if err := client.SendMedia(ctx, recipient, filePath, parsedCaption); err != nil {
					return err
				}
				fmt.Println("Media sent.")
				return nil
			})
		},
	}
	command.Flags().StringVarP(&caption, "caption", "c", "", "caption for the media file")
	return command
}

func syncCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [chat]",
		Short: "Fetch latest chats, contacts, app state, and message history from WhatsApp",
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeContacts(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
				target := ""
				if len(args) > 0 {
					target = args[0]
				}
				return handleSync(ctx, client, target)
			})
		},
	}
}

func handleSync(ctx context.Context, client *whatsapp.Client, targetArg string) error {
	pb := NewProgressBar(os.Stdout)
	targetArg = strings.TrimSpace(targetArg)
	if targetArg != "" {
		target, err := client.FindContactTarget(ctx, targetArg)
		if err != nil {
			pb.StopWithError(fmt.Sprintf("Find contact failed: %v", err))
			return err
		}
		pb.SetText(fmt.Sprintf("Syncing app state for %s...", target.DisplayName))
		_ = client.SyncAppState(ctx, false)
		if err := client.SyncChatWithProgress(ctx, target.PrimaryJID, 50, func(p whatsapp.SyncProgress) {
			pb.Update(p)
		}); err != nil {
			pb.StopWithError(fmt.Sprintf("Sync failed: %v", err))
			return err
		}
		pb.StopWithSuccess(fmt.Sprintf("Sync complete for %s", target.DisplayName))
		fmt.Println()
		printRecentMessages(client, target, 10)
		return nil
	}

	count, err := client.SyncAllWithProgress(ctx, func(p whatsapp.SyncProgress) {
		pb.Update(p)
	})
	if err != nil {
		pb.StopWithError(fmt.Sprintf("Sync failed: %v", err))
		return err
	}
	pb.StopWithSuccess(fmt.Sprintf("Sync complete! Updated app state and synced %d active chats", count))
	fmt.Println()
	printChatsList(ctx, client, 15)
	return nil
}

func startInteractiveSync(ctx context.Context, client *whatsapp.Client, rl *readline.Instance, target string) {
	target = strings.TrimSpace(target)
	if client.IsSyncing() {
		status := client.SyncStatus()
		elapsed := time.Since(status.StartTime).Truncate(100 * time.Millisecond)
		desc := status.Progress.Description
		if desc == "" {
			desc = status.Progress.StageName
		}
		fmt.Printf("Sync is already running in background: [%d/%d %s] %s (%s elapsed)\n",
			status.Progress.StageIndex, status.Progress.StageTotal, status.Progress.StageName, desc, elapsed)
		return
	}

	fmt.Println("Background sync started.")
	fmt.Println("You can continue typing commands (chats, send, inspect, status, etc.) while sync runs.")

	frameIdx := 0
	ticker := time.NewTicker(120 * time.Millisecond)
	stopTicker := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		for {
			select {
			case <-stopTicker:
				ticker.Stop()
				rl.SetPrompt(">> ")
				rl.Refresh()
				return
			case <-ticker.C:
				status := client.SyncStatus()
				if !status.IsRunning {
					return
				}
				frameIdx = (frameIdx + 1) % len(spinnerFrames)
				frame := spinnerFrames[frameIdx]
				var promptStr string
				if status.Progress.StageTotal > 0 && status.Progress.Total > 0 {
					pct := float64(status.Progress.Current) / float64(status.Progress.Total) * 100
					promptStr = fmt.Sprintf("[Sync %s %d/%d %3.0f%% (%d/%d)] >> ",
						frame, status.Progress.StageIndex, status.Progress.StageTotal, pct, status.Progress.Current, status.Progress.Total)
				} else if status.Progress.StageName != "" {
					promptStr = fmt.Sprintf("[Sync %s %s] >> ", frame, status.Progress.StageName)
				} else {
					promptStr = fmt.Sprintf("[Sync %s] >> ", frame)
				}
				rl.SetPrompt(promptStr)
				rl.Refresh()
			}
		}
	}()

	startTime := time.Now()
	_, _ = client.StartBackgroundSync(ctx, target, nil, func(synced int, err error) {
		stopOnce.Do(func() {
			close(stopTicker)
		})
		elapsed := time.Since(startTime).Truncate(100 * time.Millisecond)
		if err != nil {
			fmt.Fprintf(rl.Stdout(), "\n✖ WhatsApp sync failed: %v (%s)\n", err, elapsed)
		} else if target != "" {
			fmt.Fprintf(rl.Stdout(), "\n✔ WhatsApp sync complete for %s! (%s)\n", target, elapsed)
		} else {
			fmt.Fprintf(rl.Stdout(), "\n✔ WhatsApp sync complete! Synced %d active chats. (%s)\n", synced, elapsed)
		}
		rl.Refresh()
	})
}

func startFallbackSync(ctx context.Context, client *whatsapp.Client, target string) {
	target = strings.TrimSpace(target)
	if client.IsSyncing() {
		status := client.SyncStatus()
		elapsed := time.Since(status.StartTime).Truncate(100 * time.Millisecond)
		fmt.Printf("Sync is already running: [%d/%d %s] (%s elapsed)\n",
			status.Progress.StageIndex, status.Progress.StageTotal, status.Progress.StageName, elapsed)
		return
	}

	fmt.Println("Background sync started. You can continue typing commands.")
	startTime := time.Now()
	_, _ = client.StartBackgroundSync(ctx, target, nil, func(synced int, err error) {
		elapsed := time.Since(startTime).Truncate(100 * time.Millisecond)
		if err != nil {
			fmt.Printf("\n✖ WhatsApp sync failed: %v (%s)\n>> ", err, elapsed)
		} else if target != "" {
			fmt.Printf("\n✔ WhatsApp sync complete for %s! (%s)\n>> ", target, elapsed)
		} else {
			fmt.Printf("\n✔ WhatsApp sync complete! Synced %d active chats. (%s)\n>> ", synced, elapsed)
		}
	})
}

func printChatsList(ctx context.Context, client *whatsapp.Client, limit int) {
	if limit <= 0 {
		limit = 15
	}
	seen := map[string]time.Time{}
	for _, message := range client.Messages() {
		chatKey := message.Chat.ToNonAD().String()
		if message.At.After(seen[chatKey]) {
			seen[chatKey] = message.At
		}
	}
	if groups, err := client.WhatsApp().GetJoinedGroups(ctx); err == nil {
		for _, group := range groups {
			chatKey := group.JID.ToNonAD().String()
			if _, exists := seen[chatKey]; !exists {
				seen[chatKey] = time.Time{}
			}
		}
	}
	type chatItem struct {
		jid string
		at  time.Time
	}
	chats := make([]chatItem, 0, len(seen))
	for jid, at := range seen {
		chats = append(chats, chatItem{jid, at})
	}
	sort.Slice(chats, func(i, j int) bool {
		if !chats[i].at.Equal(chats[j].at) {
			return chats[i].at.After(chats[j].at)
		}
		return chats[i].jid < chats[j].jid
	})
	if len(chats) == 0 {
		fmt.Println("No active chats found. Try running 'sync' to fetch from WhatsApp.")
		return
	}
	for i, chat := range chats {
		if i >= limit {
			break
		}
		target, _ := client.FindContactTarget(ctx, chat.jid)
		name := chat.jid
		if target != nil && target.DisplayName != "" {
			name = fmt.Sprintf("%s (%s)", target.DisplayName, chat.jid)
		}
		timeStr := "no messages"
		if !chat.at.IsZero() {
			timeStr = chat.at.Format("2006-01-02 15:04")
		}
		fmt.Printf("%d. %s\t[%s]\n", i+1, name, timeStr)
	}
}

func chatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "chats [count]",
		Short: "Show recent active chats",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit := 15
			if len(args) > 0 {
				if parsed, err := strconv.Atoi(args[0]); err == nil && parsed > 0 {
					limit = parsed
				}
			}
			return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
				printChatsList(ctx, client, limit)
				return nil
			})
		},
	}
}

func inspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <chat> [count]",
		Short: "Show the latest messages in a chat (combined sent & received)",
		Args:  cobra.MinimumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeContacts(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			chat, count, err := parseInspectArgs(args)
			if err != nil {
				return err
			}
			return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
				target, err := client.FindContactTarget(ctx, chat)
				if err != nil {
					return err
				}
				_ = client.RequestRecentHistory(ctx, target.PrimaryJID, max(count, 50))
				printRecentMessages(client, target, count)
				return nil
			})
		},
	}
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func parseInspectArgs(args []string) (string, int, error) {
	if len(args) == 0 {
		return "", 0, errors.New("usage: inspect <contact> [count]")
	}
	countArgs := []string(nil)
	chatArgs := args
	if len(args) >= 2 {
		if _, err := strconv.Atoi(args[len(args)-1]); err == nil {
			countArgs = args[len(args)-1:]
			chatArgs = args[:len(args)-1]
		}
	}
	if len(chatArgs) == 0 {
		return "", 0, errors.New("contact is required")
	}
	count, err := messageCount(countArgs)
	if err != nil {
		return "", 0, err
	}
	chat := strings.Trim(strings.Join(chatArgs, " "), "\"'")
	if chat == "" {
		return "", 0, errors.New("contact is required")
	}
	return chat, count, nil
}

func messageCount(args []string) (int, error) {
	if len(args) == 0 {
		return 5, nil
	}
	count, err := strconv.Atoi(args[0])
	if err != nil || count < 1 {
		return 0, errors.New("count must be a positive integer")
	}
	return count, nil
}

func printRecentMessages(client *whatsapp.Client, target *whatsapp.ContactTarget, count int) {
	messages := client.MessagesForTarget(target)
	if len(messages) > count {
		messages = messages[len(messages)-count:]
	}
	if len(messages) == 0 {
		fmt.Println("No messages found in this chat.")
		return
	}
	for _, message := range messages {
		direction := "<--"
		sender := target.DisplayName
		if message.FromMe {
			direction = "-->"
			sender = "YOU"
		} else if sender == "" {
			if message.Sender.User != "" {
				sender = message.Sender.User
			} else {
				sender = "Contact"
			}
		}
		fmt.Printf("[%s] %s %s\n    %s\n", message.At.Format("2006-01-02 15:04"), sender, direction, message.Text)
	}
}

func runConnection(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir := getDataDir()
	client, err := whatsapp.New(ctx, dataDir)
	if err != nil {
		return err
	}
	defer client.Close()

	printHeader()
	if err := client.Connect(ctx, true); err != nil {
		return err
	}

	fmt.Println("WhatsApp connected.")
	if client.WhatsApp().Store.ID != nil {
		fmt.Printf("Account: %s\n", client.WhatsApp().Store.ID.String())
	}

	return runInteractiveShell(ctx, client)
}

func tokenizeLine(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune

	for _, r := range line {
		switch {
		case inQuote:
			if r == quoteChar {
				inQuote = false
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = true
			quoteChar = r
		case r == ' ' || r == '\t':
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func scheduleCommand() *cobra.Command {
	var (
		atTime   string
		inTime   string
		filePath string
		caption  string
		startBg  bool
	)

	cmd := &cobra.Command{
		Use:   "schedule <recipient> [message]",
		Short: "Schedule a message or media to be sent at a future time",
		Args:  cobra.MinimumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeContacts(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(c *cobra.Command, args []string) error {
			if atTime == "" && inTime == "" {
				return errors.New("either --at or --in is required (e.g. --at \"15-10-2026 00:00\" or --in 2h)")
			}

			var targetTime time.Time
			var err error
			if atTime != "" {
				targetTime, err = whatsapp.ParseScheduleTime(atTime, time.Local)
			} else {
				targetTime, err = whatsapp.ParseScheduleTime("in "+inTime, time.Local)
			}
			if err != nil {
				return err
			}

			recipient := ""
			text := ""
			if filePath != "" {
				recipient = strings.Join(args, " ")
			} else {
				if len(args) < 2 {
					return errors.New("message is required unless --file is used")
				}
				recipient = args[0]
				text = strings.Join(args[1:], " ")
			}

			store, err := whatsapp.NewScheduleStore(getDataDir())
			if err != nil {
				return err
			}

			item, err := store.Add(recipient, text, filePath, caption, targetTime)
			if err != nil {
				return err
			}

			fmt.Println("Message scheduled successfully!")
			fmt.Printf("ID:           %s\n", item.ID)
			fmt.Printf("Recipient:    %s\n", item.Recipient)
			if item.Text != "" {
				fmt.Printf("Message:      %s\n", item.Text)
			}
			if item.MediaPath != "" {
				fmt.Printf("Media:        %s\n", item.MediaPath)
			}
			fmt.Printf("Scheduled At: %s (%s from now)\n", item.ScheduledAt.Format("2006-01-02 15:04:05"), time.Until(item.ScheduledAt).Round(time.Minute))

			if startBg {
				if err := startBackgroundDaemonProcess(); err != nil {
					// It's ok if daemon is already running or cannot be spawned
				} else {
					fmt.Println("Background daemon launched to ensure delivery when terminal is closed.")
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&atTime, "at", "", "absolute time (e.g. '15-10-2026 00:00', 'tomorrow 09:00', '18:00')")
	cmd.Flags().StringVar(&inTime, "in", "", "relative duration (e.g. '10m', '2h', '1d')")
	cmd.Flags().StringVar(&filePath, "file", "", "media or document file to schedule")
	cmd.Flags().StringVar(&caption, "caption", "", "caption for scheduled media")
	cmd.Flags().BoolVar(&startBg, "daemon", true, "start background daemon if not already running")

	cmd.AddCommand(scheduleListCommand(), scheduleCancelCommand(), scheduleProcessCommand(), startDaemonCommand())
	return cmd
}

func scheduleListCommand() *cobra.Command {
	var includeAll bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List scheduled messages",
		RunE: func(_ *cobra.Command, _ []string) error {
			store, err := whatsapp.NewScheduleStore(getDataDir())
			if err != nil {
				return err
			}
			printScheduleList(store.List(includeAll))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&includeAll, "all", "a", false, "include completed and cancelled messages")
	return cmd
}

func scheduleCancelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a scheduled message by ID",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeScheduleIDs(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			store, err := whatsapp.NewScheduleStore(getDataDir())
			if err != nil {
				return err
			}
			if err := store.Cancel(args[0]); err != nil {
				return err
			}
			fmt.Printf("Schedule %s cancelled.\n", args[0])
			return nil
		},
	}
}

func scheduleProcessCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "process",
		Short: "Connect and process due scheduled messages now",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
				count, err := client.ProcessSchedules(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("Processed %d scheduled message(s).\n", count)
				return nil
			})
		},
	}
}

func daemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon",
		Short: "Run WhatsDown background worker for scheduled messages",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
				fmt.Println("WhatsDown background daemon is running.")
				fmt.Println("Scheduled messages will be sent automatically at their designated times.")
				fmt.Println("Press Ctrl+C to stop.")
				<-ctx.Done()
				return nil
			})
		},
	}
}

func startDaemonCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start-daemon",
		Short: "Launch background daemon process (detached, no window)",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := startBackgroundDaemonProcess(); err != nil {
				return err
			}
			fmt.Println("Background daemon started successfully.")
			return nil
		},
	}
}

func startBackgroundDaemonProcess() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Dir = filepath.Dir(exe)
	setDetachedProcess(cmd)
	return cmd.Start()
}

func printScheduleList(items []whatsapp.ScheduledMessage) {
	if len(items) == 0 {
		fmt.Println("No scheduled messages found.")
		return
	}
	fmt.Printf("%-18s %-20s %-20s %-12s %s\n", "ID", "RECIPIENT", "SCHEDULED AT", "STATUS", "MESSAGE")
	fmt.Println(strings.Repeat("-", 90))
	for _, item := range items {
		msg := item.Text
		if item.MediaPath != "" {
			if item.Caption != "" {
				msg = fmt.Sprintf("[%s] %s", filepath.Base(item.MediaPath), item.Caption)
			} else {
				msg = fmt.Sprintf("[%s]", filepath.Base(item.MediaPath))
			}
		}
		if len(msg) > 30 {
			msg = msg[:27] + "..."
		}
		fmt.Printf("%-18s %-20s %-20s %-12s %s\n",
			item.ID,
			truncateStr(item.Recipient, 20),
			item.ScheduledAt.Format("2006-01-02 15:04"),
			string(item.Status),
			msg,
		)
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func createInteractiveCompleter(_ *whatsapp.Client) *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("send",
			readline.PcItem("<recipient> \"<message>\""),
		),
		readline.PcItem("media",
			readline.PcItem("<recipient> <file-path> [caption]"),
		),
		readline.PcItem("schedule",
			readline.PcItem("<recipient> \"<message>\" --at \"<time>\""),
			readline.PcItem("<recipient> \"<message>\" --in <duration>"),
			readline.PcItem("<recipient> --file <path> --caption \"<text>\" --at \"<time>\""),
			readline.PcItem("list"),
			readline.PcItem("cancel"),
		),
		readline.PcItem("schedules"),
		readline.PcItem("cancel",
			readline.PcItem("<schedule-id>"),
		),
		readline.PcItem("inspect",
			readline.PcItem("<contact> [count]"),
		),
		readline.PcItem("chats"),
		readline.PcItem("sync",
			readline.PcItem("<contact>"),
		),
		readline.PcItem("status"),
		readline.PcItem("setup"),
		readline.PcItem("daemon"),
		readline.PcItem("quit"),
	)
}

func runInteractiveShell(ctx context.Context, client *whatsapp.Client) error {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          ">> ",
		AutoComplete:    createInteractiveCompleter(client),
		InterruptPrompt: "^C",
		EOFPrompt:       "quit",
	})
	if err != nil {
		return runFallbackInteractiveShell(ctx, client)
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if errors.Is(err, readline.ErrInterrupt) || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		parts := tokenizeLine(line)
		if len(parts) == 0 {
			continue
		}
		command := strings.ToLower(parts[0])
		switch command {
		case "":
			continue
		case "help":
			printHelp()
		case "send":
			if len(parts) < 3 {
				fmt.Println("Usage: send <phone/contact> <message>")
				break
			}
			recipient, text, err := splitRecipientMessage(ctx, client, parts[1:])
			if err != nil {
				fmt.Printf("Send failed: %v\n", err)
				break
			}
			if err := client.SendText(ctx, recipient, text); err != nil {
				fmt.Printf("Send failed: %v\n", err)
				break
			}
			fmt.Println("Message sent.")
		case "media":
			if len(parts) < 3 {
				fmt.Println("Usage: media <phone/contact> <file-path> [caption]")
				break
			}
			recipient, filePath, caption, err := parseMediaArgs(ctx, client, parts[1:])
			if err != nil {
				fmt.Printf("Media failed: %v\n", err)
				break
			}
			if err := client.SendMedia(ctx, recipient, filePath, caption); err != nil {
				fmt.Printf("Media failed: %v\n", err)
				break
			}
			fmt.Println("Media sent.")
		case "schedule":
			handleInteractiveSchedule(ctx, client, parts[1:])
		case "schedules":
			printScheduleList(client.Schedules().List(false))
		case "cancel":
			if len(parts) < 2 {
				fmt.Println("Usage: cancel <schedule-id>")
				break
			}
			if err := client.Schedules().Cancel(parts[1]); err != nil {
				fmt.Printf("Cancel failed: %v\n", err)
				break
			}
			fmt.Printf("Schedule %s cancelled.\n", parts[1])
		case "status":
			printStatus(client)
		case "inspect":
			if len(parts) < 2 {
				fmt.Println("Usage: inspect <contact> [count]")
				break
			}
			chat, count, err := parseInspectArgs(parts[1:])
			if err != nil {
				fmt.Printf("Inspect failed: %v\n", err)
				break
			}
			target, err := client.FindContactTarget(ctx, chat)
			if err != nil {
				fmt.Printf("Inspect failed: %v\n", err)
				break
			}
			_ = client.RequestRecentHistory(ctx, target.PrimaryJID, max(count, 50))
			printRecentMessages(client, target, count)
		case "sync":
			targetName := ""
			if len(parts) > 1 {
				targetName = strings.Join(parts[1:], " ")
			}
			startInteractiveSync(ctx, client, rl, targetName)
		case "chats":
			limit := 15
			if len(parts) > 1 {
				if parsedLimit, err := strconv.Atoi(parts[1]); err == nil && parsedLimit > 0 {
					limit = parsedLimit
				}
			}
			printChatsList(ctx, client, limit)
		case "daemon":
			fmt.Println("Note: The scheduler daemon is already active inside this interactive session.")
			fmt.Println("To run it in the background when the terminal is closed, run 'pstw daemon' from your system shell.")
		case "setup":
			fmt.Println("This account is already connected. Type quit or press Ctrl+C to disconnect.")
		case "quit":
			return nil
		default:
			fmt.Printf("Unknown command %q. Type help to see available commands.\n", command)
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
}

func runFallbackInteractiveShell(ctx context.Context, client *whatsapp.Client) error {
	input := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(">> ")
		if !input.Scan() {
			if err := input.Err(); err != nil {
				return err
			}
			return nil
		}

		line := strings.TrimSpace(input.Text())
		parts := tokenizeLine(line)
		if len(parts) == 0 {
			continue
		}
		command := strings.ToLower(parts[0])
		switch command {
		case "":
			continue
		case "help":
			printHelp()
		case "send":
			if len(parts) < 3 {
				fmt.Println("Usage: send <phone/contact> <message>")
				break
			}
			recipient, text, err := splitRecipientMessage(ctx, client, parts[1:])
			if err != nil {
				fmt.Printf("Send failed: %v\n", err)
				break
			}
			if err := client.SendText(ctx, recipient, text); err != nil {
				fmt.Printf("Send failed: %v\n", err)
				break
			}
			fmt.Println("Message sent.")
		case "media":
			if len(parts) < 3 {
				fmt.Println("Usage: media <phone/contact> <file-path> [caption]")
				break
			}
			recipient, filePath, caption, err := parseMediaArgs(ctx, client, parts[1:])
			if err != nil {
				fmt.Printf("Media failed: %v\n", err)
				break
			}
			if err := client.SendMedia(ctx, recipient, filePath, caption); err != nil {
				fmt.Printf("Media failed: %v\n", err)
				break
			}
			fmt.Println("Media sent.")
		case "schedule":
			handleInteractiveSchedule(ctx, client, parts[1:])
		case "schedules":
			printScheduleList(client.Schedules().List(false))
		case "cancel":
			if len(parts) < 2 {
				fmt.Println("Usage: cancel <schedule-id>")
				break
			}
			if err := client.Schedules().Cancel(parts[1]); err != nil {
				fmt.Printf("Cancel failed: %v\n", err)
				break
			}
			fmt.Printf("Schedule %s cancelled.\n", parts[1])
		case "status":
			printStatus(client)
		case "inspect":
			if len(parts) < 2 {
				fmt.Println("Usage: inspect <contact> [count]")
				break
			}
			chat, count, err := parseInspectArgs(parts[1:])
			if err != nil {
				fmt.Printf("Inspect failed: %v\n", err)
				break
			}
			target, err := client.FindContactTarget(ctx, chat)
			if err != nil {
				fmt.Printf("Inspect failed: %v\n", err)
				break
			}
			_ = client.RequestRecentHistory(ctx, target.PrimaryJID, max(count, 50))
			printRecentMessages(client, target, count)
		case "sync":
			targetName := ""
			if len(parts) > 1 {
				targetName = strings.Join(parts[1:], " ")
			}
			startFallbackSync(ctx, client, targetName)
		case "chats":
			limit := 15
			if len(parts) > 1 {
				if parsedLimit, err := strconv.Atoi(parts[1]); err == nil && parsedLimit > 0 {
					limit = parsedLimit
				}
			}
			printChatsList(ctx, client, limit)
		case "daemon":
			fmt.Println("Note: The scheduler daemon is already active inside this interactive session.")
			fmt.Println("To run it in the background when the terminal is closed, run 'pstw daemon' from your system shell.")
		case "setup":
			fmt.Println("This account is already connected. Type quit or press Ctrl+C to disconnect.")
		case "quit":
			return nil
		default:
			fmt.Printf("Unknown command %q. Type help to see available commands.\n", command)
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
}

func handleInteractiveSchedule(ctx context.Context, client *whatsapp.Client, parts []string) {
	if len(parts) == 0 || parts[0] == "list" {
		printScheduleList(client.Schedules().List(false))
		return
	}

	if parts[0] == "cancel" {
		if len(parts) < 2 {
			fmt.Println("Usage: schedule cancel <id>")
			return
		}
		if err := client.Schedules().Cancel(parts[1]); err != nil {
			fmt.Printf("Cancel failed: %v\n", err)
			return
		}
		fmt.Printf("Schedule %s cancelled.\n", parts[1])
		return
	}

	if parts[0] == "process" {
		count, err := client.ProcessSchedules(ctx)
		if err != nil {
			fmt.Printf("Process failed: %v\n", err)
			return
		}
		fmt.Printf("Processed %d scheduled message(s).\n", count)
		return
	}

	var atStr, inStr, fileStr, captionStr string
	var remaining []string
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if p == "--at" && i+1 < len(parts) {
			atStr = parts[i+1]
			i++
		} else if strings.HasPrefix(p, "--at=") {
			atStr = strings.TrimPrefix(p, "--at=")
		} else if p == "--in" && i+1 < len(parts) {
			inStr = parts[i+1]
			i++
		} else if strings.HasPrefix(p, "--in=") {
			inStr = strings.TrimPrefix(p, "--in=")
		} else if p == "--file" && i+1 < len(parts) {
			fileStr = parts[i+1]
			i++
		} else if strings.HasPrefix(p, "--file=") {
			fileStr = strings.TrimPrefix(p, "--file=")
		} else if p == "--caption" && i+1 < len(parts) {
			captionStr = parts[i+1]
			i++
		} else if strings.HasPrefix(p, "--caption=") {
			captionStr = strings.TrimPrefix(p, "--caption=")
		} else {
			remaining = append(remaining, p)
		}
	}

	if atStr == "" && inStr == "" {
		fmt.Println("Usage: schedule <recipient> <message> --at \"<time>\" (or --in <duration>)")
		fmt.Println("Example: schedule 8005331766 \"Happy Birthday!\" --at \"15-10-2026 00:00\"")
		fmt.Println("Example: schedule \"Yash Chatrath\" \"Meeting in 2h\" --in 2h")
		return
	}

	var targetTime time.Time
	var err error
	if atStr != "" {
		targetTime, err = whatsapp.ParseScheduleTime(atStr, time.Local)
	} else {
		targetTime, err = whatsapp.ParseScheduleTime("in "+inStr, time.Local)
	}
	if err != nil {
		fmt.Printf("Schedule failed: %v\n", err)
		return
	}

	if len(remaining) < 1 && fileStr == "" {
		fmt.Println("Usage: schedule <recipient> <message> --at \"<time>\"")
		return
	}

	recipient := ""
	text := ""
	if fileStr != "" {
		recipient = strings.Join(remaining, " ")
	} else {
		if len(remaining) < 2 {
			fmt.Println("Recipient and message text are required.")
			return
		}
		recipient, text, err = splitRecipientMessage(ctx, client, remaining)
		if err != nil {
			recipient = remaining[0]
			text = strings.Join(remaining[1:], " ")
		}
	}

	item, err := client.Schedules().Add(recipient, text, fileStr, captionStr, targetTime)
	if err != nil {
		fmt.Printf("Schedule failed: %v\n", err)
		return
	}

	fmt.Println("Message scheduled successfully!")
	fmt.Printf("ID:           %s\n", item.ID)
	fmt.Printf("Recipient:    %s\n", item.Recipient)
	if item.Text != "" {
		fmt.Printf("Message:      %s\n", item.Text)
	}
	if item.MediaPath != "" {
		fmt.Printf("Media:        %s\n", item.MediaPath)
	}
	fmt.Printf("Scheduled At: %s (%s from now)\n", item.ScheduledAt.Format("2006-01-02 15:04:05"), time.Until(item.ScheduledAt).Round(time.Minute))
}

func printHeader() {
	fmt.Println()
	fmt.Println("Personal WhatsApp CLI")
	fmt.Println("Type help to explore the features.")
	fmt.Println()
}

func printHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  help      Show this help message")
	fmt.Println("  send      Send a text message: send <phone/contact> <message>")
	fmt.Println("  media     Send media/file: media <phone/contact> <file-path> [caption]")
	fmt.Println("  schedule  Schedule a message: schedule <recipient> <message> --at \"<time>\"")
	fmt.Println("  schedules List scheduled messages")
	fmt.Println("  cancel    Cancel a scheduled message: cancel <schedule-id>")
	fmt.Println("  inspect   Show recent messages: inspect <contact> [count]")
	fmt.Println("  chats     Show recent active chats: chats [count]")
	fmt.Println("  sync      Sync latest messages, chats & contacts: sync [contact]")
	fmt.Println("  status    Show the connected WhatsApp account")
	fmt.Println("  setup     Show pairing status")
	fmt.Println("  quit      Disconnect and exit WhatsDown")
}

func printStatus(client *whatsapp.Client) {
	if client.WhatsApp().Store.ID == nil {
		fmt.Println("Status: connected, account not available")
		return
	}
	fmt.Printf("Status:   connected\nAccount:  %s\n", client.WhatsApp().Store.ID.String())
	status := client.SyncStatus()
	if status.IsRunning {
		elapsed := time.Since(status.StartTime).Truncate(100 * time.Millisecond)
		if status.Progress.StageName != "" {
			if status.Progress.Total > 0 {
				percent := float64(status.Progress.Current) / float64(status.Progress.Total) * 100
				fmt.Printf("Sync:     🔄 In progress ([%d/%d] %s: %.0f%% (%d/%d) • %s, elapsed %s)\n",
					status.Progress.StageIndex, status.Progress.StageTotal, status.Progress.StageName,
					percent, status.Progress.Current, status.Progress.Total, status.Progress.Description, elapsed)
			} else {
				fmt.Printf("Sync:     🔄 In progress ([%d/%d] %s: %s, elapsed %s)\n",
					status.Progress.StageIndex, status.Progress.StageTotal, status.Progress.StageName, status.Progress.Description, elapsed)
			}
		} else {
			fmt.Printf("Sync:     🔄 In progress (elapsed %s)\n", elapsed)
		}
	} else if !status.LastSync.IsZero() {
		fmt.Printf("Sync:     Idle (last synced %s, updated %d chats)\n", status.LastSync.Format("15:04:05"), status.SyncedCount)
	}
}

func splitRecipientMessage(ctx context.Context, client *whatsapp.Client, args []string) (string, string, error) {
	if len(args) < 2 {
		return "", "", errors.New("usage: send <recipient> <message>")
	}
	for split := len(args) - 1; split > 0; split-- {
		recipient := strings.Join(args[:split], " ")
		if _, err := client.FindContactTarget(ctx, recipient); err == nil {
			return recipient, strings.Join(args[split:], " "), nil
		}
	}
	return "", "", fmt.Errorf("contact not found: %s", strings.Join(args[:len(args)-1], " "))
}

func parseMediaArgs(ctx context.Context, client *whatsapp.Client, args []string) (string, string, string, error) {
	if len(args) < 2 {
		return "", "", "", errors.New("usage: media <contact> <file-path> [caption]")
	}

	// 1. Try finding a split where args[i] or args[i:j] is an existing file on disk
	for i := 1; i < len(args); i++ {
		recipientCandidate := strings.Join(args[:i], " ")
		for j := i + 1; j <= len(args); j++ {
			pathCandidate := strings.Trim(strings.Join(args[i:j], " "), `"'`)
			if fi, err := os.Stat(pathCandidate); err == nil && !fi.IsDir() {
				caption := strings.Join(args[j:], " ")
				return recipientCandidate, pathCandidate, caption, nil
			}
		}
	}

	// 2. Fallback: try finding a matching contact for the recipient prefix
	for split := len(args) - 1; split > 0; split-- {
		recipient := strings.Join(args[:split], " ")
		if _, err := client.FindContactTarget(ctx, recipient); err == nil {
			filePath := strings.Trim(args[split], `"'`)
			caption := strings.Join(args[split+1:], " ")
			return recipient, filePath, caption, nil
		}
	}

	// 3. Default fallback
	recipient := args[0]
	filePath := strings.Trim(args[1], `"'`)
	caption := strings.Join(args[2:], " ")
	return recipient, filePath, caption, nil
}

func completionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for WhatsDown (PSTW).

To load completions:

PowerShell:
  # To load completions in your current PowerShell session:
  pstw completion powershell | Out-String | Invoke-Expression

  # To load completions for every new PowerShell session, run:
  Add-Content -Path $PROFILE -Value "pstw completion powershell | Out-String | Invoke-Expression"

Bash:
  # To load completions in your current bash session:
  source <(pstw completion bash)

  # To load completions for every new bash session, run:
  pstw completion bash > /etc/bash_completion.d/pstw

Zsh:
  # To load completions in your current zsh session:
  source <(pstw completion zsh)

  # To load completions for every new zsh session, run:
  pstw completion zsh > "${fpath[1]}/_pstw"

Fish:
  # To load completions in your current fish session:
  pstw completion fish | source

  # To load completions for every new fish session, run:
  pstw completion fish > ~/.config/fish/completions/pstw.fish
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(os.Stdout)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell %q", args[0])
			}
		},
	}
	return cmd
}

func completeContacts(toComplete string) []string {
	var suggestions []string
	seen := make(map[string]bool)

	// 1. From schedules
	if store, err := whatsapp.NewScheduleStore(getDataDir()); err == nil {
		for _, s := range store.List(true) {
			rec := strings.TrimSpace(s.Recipient)
			if rec != "" && !seen[rec] {
				seen[rec] = true
				if strings.HasPrefix(strings.ToLower(rec), strings.ToLower(toComplete)) {
					suggestions = append(suggestions, rec)
				}
			}
		}
	}

	// 2. From messages.json (ignore group chats)
	if data, err := os.ReadFile(filepath.Join(getDataDir(), "messages.json")); err == nil {
		var messages []whatsapp.Message
		if err := json.Unmarshal(data, &messages); err == nil {
			for _, m := range messages {
				if m.Chat.Server == "g.us" || strings.HasPrefix(m.Chat.User, "120363") {
					continue
				}
				chatUser := m.Chat.User
				if chatUser != "" && !seen[chatUser] {
					seen[chatUser] = true
					if strings.HasPrefix(chatUser, toComplete) {
						suggestions = append(suggestions, chatUser)
					}
				}
			}
		}
	}

	return suggestions
}

func completeScheduleIDs(toComplete string) []string {
	var suggestions []string
	if store, err := whatsapp.NewScheduleStore(getDataDir()); err == nil {
		for _, item := range store.List(false) {
			if strings.HasPrefix(item.ID, toComplete) {
				desc := item.Recipient
				if item.Text != "" {
					desc += ": " + item.Text
				}
				if len(desc) > 30 {
					desc = desc[:27] + "..."
				}
				suggestions = append(suggestions, fmt.Sprintf("%s\t%s", item.ID, desc))
			}
		}
	}
	return suggestions
}




