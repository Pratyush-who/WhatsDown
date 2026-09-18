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
	root.AddCommand(setupCommand(), statusCommand(), sendCommand(), contactsCommand(), checkCommand(), chatsCommand(), messagesCommand(), replyCommand(), reactCommand(), searchCommand(), listenCommand())
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
					if err := client.SendFile(ctx, args[0], filePath); err != nil {
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
	command.Flags().StringVar(&filePath, "file", "", "send a document from this path")
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
		return withClient(cmd, false, func(_ context.Context, client *whatsapp.Client) error {
			seen := map[string]time.Time{}
			for _, message := range client.Messages() {
				if message.At.After(seen[message.Chat.String()]) {
					seen[message.Chat.String()] = message.At
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
				if i == 5 {
					break
				}
				fmt.Printf("%d. %s\t%s\n", i+1, chat.jid, chat.at.Format(time.RFC3339))
			}
			return nil
		})
	}}
}

func messagesCommand() *cobra.Command {
	return &cobra.Command{Use: "messages <chat>", Short: "Show messages seen during this session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withClient(cmd, false, func(ctx context.Context, client *whatsapp.Client) error {
			jid, err := client.ResolveRecipient(ctx, args[0])
			if err != nil {
				return err
			}
			count := 0
			for _, message := range client.Messages() {
				if message.Chat == jid {
					fmt.Printf("[%d] %s\t%s\n    %s\n", message.Index, message.At.Format("15:04"), message.Sender.String(), message.Text)
					count++
				}
			}
			if count == 0 {
				fmt.Println("No messages seen in this session.")
			}
			return nil
		})
	}}
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
		parts := strings.Fields(line)
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
				fmt.Println("Usage: send <phone> <message>")
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
		case "status":
			printStatus(client)
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
	fmt.Println("WhatsDown")
	fmt.Println("Personal WhatsApp CLI")
	fmt.Println("Type help to explore the features.")
	fmt.Println("Press Ctrl+C to disconnect.")
	fmt.Println()
}

func printHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  help      Show this help message")
	fmt.Println("  send      Send a message: send <phone> <message>")
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
		if _, err := client.ResolveRecipient(ctx, recipient); err == nil {
			return recipient, strings.Join(args[split:], " "), nil
		}
	}
	return "", "", fmt.Errorf("contact not found: %s", strings.Join(args[:len(args)-1], " "))
}
