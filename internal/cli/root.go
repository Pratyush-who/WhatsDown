package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Pratyush-who/WhatsDown/internal/whatsapp"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "pstw",
		Short: "Personal WhatsApp terminal client",
		RunE:  runConnection,
	}
	root.AddCommand(setupCommand(), statusCommand(), sendCommand(), mediaCommand(), contactsCommand(), checkCommand(), chatsCommand(), messagesCommand(), inspectCommand(), replyCommand(), reactCommand(), searchCommand(), listenCommand())
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

func withClient(cmd *cobra.Command, renderQR bool, action func(context.Context, *whatsapp.Client) error) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := whatsapp.New(ctx, filepath.Join("data"))
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

func contactsCommand() *cobra.Command {
	return &cobra.Command{Use: "contacts [search]", Short: "List synced contacts", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		query := ""
		if len(args) == 1 {
			query = args[0]
		}
		return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
			contacts, err := client.Contacts(ctx)
			if err != nil {
				return err
			}
			for jid, contact := range contacts {
				name := contact.FullName
				if name == "" {
					name = contact.PushName
				}
				if query == "" || strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
					fmt.Printf("%s\t%s\n", name, jid.String())
				}
			}
			return nil
		})
	}}
}

func checkCommand() *cobra.Command {
	return &cobra.Command{Use: "check <phone-number>", Short: "Check whether a number uses WhatsApp", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
			jid, ok, err := client.CheckNumber(ctx, args[0])
			if err != nil {
				return err
			}
			if ok {
				fmt.Printf("WhatsApp: YES (%s)\n", jid.String())
			} else {
				fmt.Println("WhatsApp: NO")
			}
			return nil
		})
	}}
}

func chatsCommand() *cobra.Command {
	return &cobra.Command{Use: "chats", Short: "Show chats seen during this session", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
			seen := map[string]time.Time{}
			for _, message := range client.Messages() {
				chatKey := message.Chat.ToNonAD().String()
				if message.At.After(seen[chatKey]) {
					seen[chatKey] = message.At
				}
			}
			type chat struct {
				jid string
				at  time.Time
			}
			chats := make([]chat, 0, len(seen))
			for jid, at := range seen {
				chats = append(chats, chat{jid, at})
			}
			sort.Slice(chats, func(i, j int) bool { return chats[i].at.After(chats[j].at) })
			for i, chat := range chats {
				if i == 10 {
					break
				}
				target, _ := client.FindContactTarget(ctx, chat.jid)
				name := chat.jid
				if target != nil && target.DisplayName != "" {
					name = fmt.Sprintf("%s (%s)", target.DisplayName, chat.jid)
				}
				fmt.Printf("%d. %s\t[%s]\n", i+1, name, chat.at.Format("2006-01-02 15:04"))
			}
			return nil
		})
	}}
}

func messagesCommand() *cobra.Command {
	return &cobra.Command{Use: "messages <chat>", Short: "Show messages seen during this session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
			target, err := client.FindContactTarget(ctx, args[0])
			if err != nil {
				return err
			}
			messages := client.MessagesForTarget(target)
			if len(messages) == 0 {
				fmt.Println("No messages seen for this contact.")
				return nil
			}
			for _, message := range messages {
				direction := "<--"
				sender := target.DisplayName
				if message.FromMe {
					direction = "-->"
					sender = "YOU"
				}
				fmt.Printf("[%d] %s\t%s %s\n    %s\n", message.Index, message.At.Format("15:04"), sender, direction, message.Text)
			}
			return nil
		})
	}}
}

func inspectCommand() *cobra.Command {
	return &cobra.Command{Use: "inspect <chat> [count]", Short: "Show the latest messages in a chat (combined sent & received)", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		chat, count, err := parseInspectArgs(args)
		if err != nil {
			return err
		}
		return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
			target, err := client.FindContactTarget(ctx, chat)
			if err != nil {
				return err
			}
			_ = client.RequestHistory(ctx, target.PrimaryJID, max(count, 50))
			printRecentMessages(client, target, count)
			return nil
		})
	}}
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

func replyCommand() *cobra.Command {
	return &cobra.Command{Use: "reply <chat> [message-number] <message>", Short: "Reply to a recent message", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		index := 0
		textStart := 1
		if len(args) > 2 {
			parsed, err := strconv.Atoi(args[1])
			if err == nil {
				index = parsed
				textStart = 2
			}
		}
		return withClient(cmd, true, func(ctx context.Context, client *whatsapp.Client) error {
			return client.Reply(ctx, args[0], strings.Join(args[textStart:], " "), index)
		})
	}}
}

func reactCommand() *cobra.Command {
	return &cobra.Command{Use: "react <chat> <message-number> <emoji>", Short: "React to a recent message", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		index, err := strconv.Atoi(args[1])
		if err != nil || index < 1 {
			return errors.New("message-number must be a positive integer")
		}
		return withClient(cmd, true, func(ctx context.Context, client *whatsapp.Client) error {
			return client.React(ctx, args[0], index, args[2])
		})
	}}
}

func searchCommand() *cobra.Command {
	return &cobra.Command{Use: "search <text>", Short: "Search messages seen during this session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withClient(cmd, false, func(_ context.Context, client *whatsapp.Client) error {
			for _, message := range client.Messages() {
				if strings.Contains(strings.ToLower(message.Text), strings.ToLower(args[0])) {
					fmt.Printf("[%d] %s: %s\n", message.Index, message.Chat.String(), message.Text)
				}
			}
			return nil
		})
	}}
}

func listenCommand() *cobra.Command {
	return &cobra.Command{Use: "listen", Short: "Listen for incoming messages", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return withClient(cmd, true, func(ctx context.Context, _ *whatsapp.Client) error {
			fmt.Println("Listening. Press Ctrl+C to stop.")
			<-ctx.Done()
			return nil
		})
	}}
}

func runConnection(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir := filepath.Join("data")
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

func runInteractiveShell(ctx context.Context, client *whatsapp.Client) error {
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
			_ = client.RequestHistory(ctx, target.PrimaryJID, max(count, 50))
			printRecentMessages(client, target, count)
		case "contacts":
			query := ""
			if len(parts) >= 2 {
				query = strings.Join(parts[1:], " ")
			}
			contacts, err := client.Contacts(ctx)
			if err != nil {
				fmt.Printf("Contacts failed: %v\n", err)
				break
			}
			for jid, contact := range contacts {
				name := contact.FullName
				if name == "" {
					name = contact.PushName
				}
				if query == "" || strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
					fmt.Printf("%s\t%s\n", name, jid.String())
				}
			}
		case "chats":
			seen := map[string]time.Time{}
			for _, message := range client.Messages() {
				chatKey := message.Chat.ToNonAD().String()
				if message.At.After(seen[chatKey]) {
					seen[chatKey] = message.At
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
			sort.Slice(chats, func(i, j int) bool { return chats[i].at.After(chats[j].at) })
			for i, ch := range chats {
				if i == 10 {
					break
				}
				target, _ := client.FindContactTarget(ctx, ch.jid)
				name := ch.jid
				if target != nil && target.DisplayName != "" {
					name = fmt.Sprintf("%s (%s)", target.DisplayName, ch.jid)
				}
				fmt.Printf("%d. %s\t[%s]\n", i+1, name, ch.at.Format("2006-01-02 15:04"))
			}
		case "check":
			if len(parts) < 2 {
				fmt.Println("Usage: check <phone-number>")
				break
			}
			jid, ok, err := client.CheckNumber(ctx, parts[1])
			if err != nil {
				fmt.Printf("Check failed: %v\n", err)
				break
			}
			if ok {
				fmt.Printf("WhatsApp: YES (%s)\n", jid.String())
			} else {
				fmt.Println("WhatsApp: NO")
			}
		case "reply":
			if len(parts) < 3 {
				fmt.Println("Usage: reply <chat> [message-number] <message>")
				break
			}
			index := 0
			textStart := 2
			if len(parts) > 3 {
				if parsed, err := strconv.Atoi(parts[2]); err == nil {
					index = parsed
					textStart = 3
				}
			}
			if err := client.Reply(ctx, parts[1], strings.Join(parts[textStart:], " "), index); err != nil {
				fmt.Printf("Reply failed: %v\n", err)
				break
			}
			fmt.Println("Reply sent.")
		case "react":
			if len(parts) < 4 {
				fmt.Println("Usage: react <chat> <message-number> <emoji>")
				break
			}
			index, err := strconv.Atoi(parts[2])
			if err != nil || index < 1 {
				fmt.Println("message-number must be a positive integer")
				break
			}
			if err := client.React(ctx, parts[1], index, parts[3]); err != nil {
				fmt.Printf("React failed: %v\n", err)
				break
			}
			fmt.Println("Reaction sent.")
		case "search":
			if len(parts) < 2 {
				fmt.Println("Usage: search <text>")
				break
			}
			query := strings.Join(parts[1:], " ")
			for _, message := range client.Messages() {
				if strings.Contains(strings.ToLower(message.Text), strings.ToLower(query)) {
					fmt.Printf("[%d] %s: %s\n", message.Index, message.Chat.String(), message.Text)
				}
			}
		case "setup":
			fmt.Println("This account is already connected. Use Ctrl+C to disconnect.")
		case "exit", "quit":
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
	fmt.Println("  inspect   Show recent messages: inspect <contact> [count]")
	fmt.Println("  chats     Show recent active chats")
	fmt.Println("  contacts  List synced contacts: contacts [search]")
	fmt.Println("  check     Check if phone number is on WhatsApp: check <phone>")
	fmt.Println("  reply     Reply to a message: reply <contact> [message-number] <message>")
	fmt.Println("  react     React to a message: react <contact> <message-number> <emoji>")
	fmt.Println("  search    Search session messages: search <text>")
	fmt.Println("  status    Show the connected WhatsApp account")
	fmt.Println("  setup     Show pairing status")
	fmt.Println("  exit      Disconnect and exit WhatsDown")
	fmt.Println("  quit      Disconnect and exit WhatsDown")
}

func printStatus(client *whatsapp.Client) {
	if client.WhatsApp().Store.ID == nil {
		fmt.Println("Status: connected, account not available")
		return
	}
	fmt.Printf("Status: connected\nAccount: %s\n", client.WhatsApp().Store.ID.String())
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


