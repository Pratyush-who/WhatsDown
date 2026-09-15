package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Pratyush-who/WhatsDown/internal/whatsapp"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "pstw",
		Short: "Personal WhatsApp terminal client",
		RunE:  runConnection,
	}
	root.AddCommand(setupCommand(), statusCommand())
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Println("PSTW")
			return runConnection(cmd, nil)
		},
	}
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

	if err := client.Connect(ctx, true); err != nil {
		return err
	}

	if client.WhatsApp().Store.ID != nil {
		fmt.Printf("Account: %s\n", client.WhatsApp().Store.ID.String())
	}
	if cmd.Name() == "status" || cmd.Name() == "pstw" || cmd.Name() == "setup" {
		fmt.Println("Press Ctrl+C to disconnect.")
		<-ctx.Done()
	}
	return nil
}
