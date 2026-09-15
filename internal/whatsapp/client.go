package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
)

var ErrNotPaired = errors.New("no WhatsApp device is paired")

type Client struct {
	container *sqlstore.Container
	device    *whatsmeow.Client
}

func New(ctx context.Context, dataDir string) (*Client, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	databasePath, err := filepath.Abs(filepath.Join(dataDir, "whatsapp.db"))
	if err != nil {
		return nil, fmt.Errorf("resolve WhatsApp database path: %w", err)
	}

	container, err := sqlstore.New(ctx, "sqlite3", sqliteAddress(databasePath), nil)
	if err != nil {
		return nil, fmt.Errorf("open WhatsApp session store: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return nil, fmt.Errorf("load WhatsApp device: %w", err)
	}

	client := &Client{container: container}
	if device == nil {
		device = container.NewDevice()
	}
	client.device = whatsmeow.NewClient(device, nil)
	return client, nil
}

func sqliteAddress(path string) string {
	return "file:" + filepath.ToSlash(path) + "?_foreign_keys=on"
}

func (client *Client) WhatsApp() *whatsmeow.Client {
	return client.device
}

func (client *Client) Close() error {
	if client.device != nil {
		client.device.Disconnect()
	}
	if client.container != nil {
		return client.container.Close()
	}
	return nil
}

func (client *Client) Connect(ctx context.Context, renderQR bool) error {
	client.device.AddEventHandler(func(event interface{}) {
		switch event.(type) {
		case *events.Connected:
			fmt.Println("WhatsApp connected.")
		case *events.LoggedOut:
			fmt.Println("WhatsApp session was logged out.")
		}
	})

	if client.device.Store.ID != nil {
		if err := client.device.ConnectContext(ctx); err != nil {
			return fmt.Errorf("connect to WhatsApp: %w", err)
		}
		return nil
	}

	qrChannel, err := client.device.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("prepare WhatsApp pairing: %w", err)
	}
	if err := client.device.ConnectContext(ctx); err != nil {
		return fmt.Errorf("connect for WhatsApp pairing: %w", err)
	}

	for item := range qrChannel {
		switch {
		case item.Event == whatsmeow.QRChannelEventCode:
			if renderQR {
				fmt.Println("\nScan this QR code in WhatsApp > Linked devices:")
				qrterminal.GenerateHalfBlock(item.Code, qrterminal.L, os.Stdout)
			}
		case item == whatsmeow.QRChannelSuccess:
			fmt.Println("Authentication successful.")
		case item.Event == whatsmeow.QRChannelEventError:
			return fmt.Errorf("WhatsApp pairing failed: %w", item.Error)
		case item == whatsmeow.QRChannelTimeout:
			return errors.New("WhatsApp pairing timed out")
		}
	}

	return nil
}
