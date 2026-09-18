package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waCommon "go.mau.fi/whatsmeow/proto/waCommon"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

var ErrNotPaired = errors.New("no WhatsApp device is paired")

type Client struct {
	container   *sqlstore.Container 
	device      *whatsmeow.Client   
	messagePath string
	mu          sync.RWMutex
	messages    []Message
}

type Message struct {
	Index  int
	ID     string
	Chat   types.JID
	Sender types.JID
	Text   string
	At     time.Time
	FromMe bool
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

	client := &Client{container: container, messagePath: filepath.Join(dataDir, "messages.json")}
	if data, readErr := os.ReadFile(client.messagePath); readErr == nil {
		_ = json.Unmarshal(data, &client.messages)
	}
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

func (client *Client) ResolveRecipient(ctx context.Context, input string) (types.JID, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return types.JID{}, errors.New("recipient is required")
	}
	if jid, err := types.ParseJID(input); err == nil && !jid.IsEmpty() && isWhatsAppServer(jid.Server) {
		return jid, nil
	}
	phone := client.normalizePhoneForAccount(input)
	if isDigits(phone) {
		contacts, err := client.device.IsOnWhatsApp(ctx, []string{"+" + phone})
		if err != nil {
			return types.JID{}, fmt.Errorf("check WhatsApp number: %w", err)
		}
		if len(contacts) > 0 && contacts[0].IsIn && !contacts[0].JID.IsEmpty() {
			return contacts[0].JID, nil
		}
	}
	contacts, err := client.device.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return types.JID{}, fmt.Errorf("load contacts: %w", err)
	}
	for jid, contact := range contacts {
		if strings.EqualFold(input, contact.FullName) || strings.EqualFold(input, contact.FirstName) || strings.EqualFold(input, contact.PushName) {
			return jid, nil
		}
	}
	groups, err := client.device.GetJoinedGroups(ctx)
	if err != nil {
		return types.JID{}, fmt.Errorf("load WhatsApp groups: %w", err)
	}
	for _, group := range groups {
		if strings.EqualFold(input, group.Name) {
			return group.JID, nil
		}
	}
	return types.JID{}, fmt.Errorf("contact not found: %s", input)
}

func (client *Client) SendText(ctx context.Context, recipient, text string) error {
	if text == "" {
		return errors.New("message is required")
	}
	jid, err := client.ResolveRecipient(ctx, recipient)
	if err != nil {
		return err
	}
	info, err := client.device.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(text),
	})
	if err != nil {
		return fmt.Errorf("send WhatsApp message: %w", err)
	}
	client.record(Message{ID: info.ID, Chat: jid, Sender: client.device.Store.ID.ToNonAD(), Text: text, At: time.Now(), FromMe: true})
	return nil
}

func (client *Client) SendFile(ctx context.Context, recipient, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	jid, err := client.ResolveRecipient(ctx, recipient)
	if err != nil {
		return err
	}
	response, err := client.device.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}
	mimetype := mime.TypeByExtension(filepath.Ext(path))
	if mimetype == "" {
		mimetype = "application/octet-stream"
	}
	name := filepath.Base(path)
	_, err = client.device.SendMessage(ctx, jid, &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL: &response.URL, DirectPath: &response.DirectPath, Mimetype: proto.String(mimetype), FileName: &name,
		FileSHA256: response.FileSHA256, FileEncSHA256: response.FileEncSHA256, MediaKey: response.MediaKey, FileLength: &response.FileLength,
	}})
	if err != nil {
		return fmt.Errorf("send file: %w", err)
	}
	return nil
}

func (client *Client) CheckNumber(ctx context.Context, input string) (types.JID, bool, error) {
	phone := client.normalizePhoneForAccount(input)
	if !isDigits(phone) {
		return types.JID{}, false, errors.New("a phone number is required")
	}
	result, err := client.device.IsOnWhatsApp(ctx, []string{"+" + phone})
	if err != nil {
		return types.JID{}, false, fmt.Errorf("check WhatsApp number: %w", err)
	}
	if len(result) == 0 || !result[0].IsIn || result[0].JID.IsEmpty() {
		return types.JID{}, false, nil
	}
	return result[0].JID, true, nil
}

func (client *Client) Contacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	return client.device.Store.Contacts.GetAllContacts(ctx)
}

func (client *Client) Messages() []Message {
	client.mu.RLock()
	defer client.mu.RUnlock()
	result := make([]Message, len(client.messages))
	copy(result, client.messages)
	return result
}

func (client *Client) Reply(ctx context.Context, recipient, text string, index int) error {
	jid, err := client.ResolveRecipient(ctx, recipient)
	if err != nil {
		return err
	}
	messages := client.Messages()
	var quoted *Message
	for i := range messages {
		if messages[i].Chat == jid && (index == 0 || messages[i].Index == index) {
			copy := messages[i]
			quoted = &copy
		}
	}
	if quoted == nil {
		return errors.New("message reference not found; run messages first")
	}
	contextInfo := &waE2E.ContextInfo{
		StanzaID:      proto.String(quoted.ID),
		Participant:   proto.String(quoted.Sender.String()),
		QuotedMessage: &waE2E.Message{Conversation: proto.String(quoted.Text)},
	}
	message := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: contextInfo}}
	info, err := client.device.SendMessage(ctx, jid, message)
	if err != nil {
		return fmt.Errorf("send reply: %w", err)
	}
	client.record(Message{ID: info.ID, Chat: jid, Sender: client.device.Store.ID.ToNonAD(), Text: text, At: time.Now(), FromMe: true})
	return nil
}

func (client *Client) React(ctx context.Context, recipient string, index int, reaction string) error {
	jid, err := client.ResolveRecipient(ctx, recipient)
	if err != nil {
		return err
	}
	var quoted *Message
	for _, message := range client.Messages() {
		if message.Chat == jid && message.Index == index {
			copy := message
			quoted = &copy
			break
		}
	}
	if quoted == nil {
		return errors.New("message reference not found; run messages first")
	}
	fromMe := quoted.FromMe
	key := &waCommon.MessageKey{RemoteJID: proto.String(quoted.Chat.String()), FromMe: &fromMe, ID: proto.String(quoted.ID)}
	if !quoted.FromMe {
		key.Participant = proto.String(quoted.Sender.String())
	}
	_, err = client.device.SendMessage(ctx, jid, &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Key: key, Text: proto.String(reaction)}})
	if err != nil {
		return fmt.Errorf("send reaction: %w", err)
	}
	return nil
}

func (client *Client) record(message Message) {
	client.mu.Lock()
	defer client.mu.Unlock()
	for index := range client.messages {
		if client.messages[index].ID == message.ID && client.messages[index].Chat == message.Chat {
			if client.messages[index].Text == "" && message.Text != "" {
				client.messages[index].Text = message.Text
				client.messages[index].At = message.At
			}
			return
		}
	}
	message.Index = len(client.messages) + 1
	client.messages = append(client.messages, message)
	data, err := json.MarshalIndent(client.messages, "", "  ")
	if err == nil {
		_ = os.WriteFile(client.messagePath, data, 0o600)
	}
}

func normalizePhone(input string) string {
	return strings.NewReplacer("+", "", " ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(input))
}

func (client *Client) normalizePhoneForAccount(input string) string {
	phone := normalizePhone(input)
	if len(phone) != 10 || !isDigits(phone) || client.device.Store.ID == nil {
		return phone
	}
	accountNumber := client.device.Store.ID.User
	if len(accountNumber) <= 10 || !isDigits(accountNumber) {
		return phone
	}
	return accountNumber[:len(accountNumber)-10] + phone
}

func isDigits(input string) bool {
	if input == "" {
		return false
	}
	for _, char := range input {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func isWhatsAppServer(server string) bool {
	return server == "s.whatsapp.net" || server == "g.us" || server == "broadcast"
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
		switch event := event.(type) {
		case *events.Message:
			if event.SourceWebMsg != nil {
				return
			}
			text := event.Message.GetConversation()
			if text == "" && event.Message.GetExtendedTextMessage() != nil {
				text = event.Message.GetExtendedTextMessage().GetText()
			}
			client.record(Message{ID: event.Info.ID, Chat: event.Info.Chat, Sender: event.Info.Sender, Text: text, At: event.Info.Timestamp, FromMe: event.Info.IsFromMe})
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
