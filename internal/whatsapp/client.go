package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var ErrNotPaired = errors.New("no WhatsApp device is paired")

type Client struct {
	container    *sqlstore.Container
	device       *whatsmeow.Client
	messagePath  string
	mu           sync.RWMutex
	messages     []Message
	messageIndex map[string]int // maps msg.ID + "\x00" + chatJID -> slice index
	saveCh       chan struct{}
	stopSave     chan struct{}
	saveWg       sync.WaitGroup
	historyDone  chan struct{}
}

func New(ctx context.Context, dataDir string) (*Client, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	databasePath, err := filepath.Abs(filepath.Join(dataDir, "whatsapp.db"))
	if err != nil {
		return nil, fmt.Errorf("resolve WhatsApp database path: %w", err)
	}

	container, err := sqlstore.New(ctx, "sqlite3", sqliteAddress(databasePath), waLog.Noop)
	if err != nil {
		return nil, fmt.Errorf("open WhatsApp session store: %w", err)
	}

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return nil, fmt.Errorf("load WhatsApp device: %w", err)
	}

	if device == nil {
		device = container.NewDevice()
	}
	device.Log = waLog.Noop

	client := &Client{
		container:    container,
		messagePath:  filepath.Join(dataDir, "messages.json"),
		messageIndex: make(map[string]int),
		saveCh:       make(chan struct{}, 1),
		stopSave:     make(chan struct{}),
		historyDone:  make(chan struct{}, 1),
	}

	if data, readErr := os.ReadFile(client.messagePath); readErr == nil {
		_ = json.Unmarshal(data, &client.messages)
		for i, msg := range client.messages {
			if msg.ID != "" {
				client.messageIndex[msg.ID+"\x00"+msg.Chat.ToNonAD().String()] = i
			}
		}
	}

	client.saveWg.Add(1)
	go client.saveLoop()

	client.device = whatsmeow.NewClient(device, waLog.Noop)
	return client, nil
}

func sqliteAddress(path string) string {
	return "file:" + filepath.ToSlash(path) + "?_foreign_keys=on"
}

func (client *Client) WhatsApp() *whatsmeow.Client {
	return client.device
}

func (client *Client) saveLoop() {
	defer client.saveWg.Done()
	var timer *time.Timer
	var timerCh <-chan time.Time

	for {
		select {
		case <-client.stopSave:
			if timer != nil {
				timer.Stop()
			}
			client.flushSave()
			return
		case <-client.saveCh:
			if timer == nil {
				timer = time.NewTimer(300 * time.Millisecond)
				timerCh = timer.C
			} else {
				timer.Reset(300 * time.Millisecond)
			}
		case <-timerCh:
			client.flushSave()
			timer = nil
			timerCh = nil
		}
	}
}

func (client *Client) scheduleSave() {
	select {
	case client.saveCh <- struct{}{}:
	default:
	}
}

func (client *Client) flushSave() {
	client.mu.RLock()
	messagesCopy := make([]Message, len(client.messages))
	copy(messagesCopy, client.messages)
	client.mu.RUnlock()

	data, err := json.MarshalIndent(messagesCopy, "", "  ")
	if err == nil {
		tmpPath := client.messagePath + ".tmp"
		if writeErr := os.WriteFile(tmpPath, data, 0o600); writeErr == nil {
			_ = os.Rename(tmpPath, client.messagePath)
		}
	}
}

func (client *Client) recordInMemory(message Message) {
	client.mu.Lock()
	defer client.mu.Unlock()

	key := message.ID + "\x00" + message.Chat.ToNonAD().String()
	if idx, exists := client.messageIndex[key]; exists && idx < len(client.messages) {
		if client.messages[idx].Text == "" && message.Text != "" {
			client.messages[idx].Text = message.Text
			client.messages[idx].At = message.At
		}
		return
	}

	message.Index = len(client.messages) + 1
	client.messageIndex[key] = len(client.messages)
	client.messages = append(client.messages, message)
}

func (client *Client) record(message Message) {
	client.recordInMemory(message)
	client.scheduleSave()
}

func (client *Client) Close() error {
	close(client.stopSave)
	client.saveWg.Wait()

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
		switch event := event.(type) {
		case *events.Message:
			text := ExtractMessageText(event.Message)
			if text != "" {
				client.record(Message{
					ID:     event.Info.ID,
					Chat:   event.Info.Chat,
					Sender: event.Info.Sender,
					Text:   text,
					At:     event.Info.Timestamp,
					FromMe: event.Info.IsFromMe,
				})
			}
		case *events.HistorySync:
			client.recordHistorySync(event.Data)
			select {
			case client.historyDone <- struct{}{}:
			default:
			}
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
	fmt.Println("Generating QR code...")
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
