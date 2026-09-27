package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waCommon "go.mau.fi/whatsmeow/proto/waCommon"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func (client *Client) SendText(ctx context.Context, recipient, text string) error {
	if text == "" {
		return errors.New("message is required")
	}
	target, err := client.FindContactTarget(ctx, recipient)
	if err != nil {
		return err
	}
	jid := target.PrimaryJID
	info, err := client.device.SendMessage(ctx, jid, &waE2E.Message{
		Conversation: proto.String(text),
	})
	if err != nil {
		return fmt.Errorf("send WhatsApp message: %w", err)
	}
	sender := types.JID{}
	if client.device.Store.ID != nil {
		sender = client.device.Store.ID.ToNonAD()
	}
	client.record(Message{ID: info.ID, Chat: jid, Sender: sender, Text: text, At: time.Now(), FromMe: true})
	return nil
}

func (client *Client) SendMedia(ctx context.Context, recipient, filePath, caption string) error {
	filePath = strings.Trim(filePath, `"'`)
	if filePath == "" {
		return errors.New("file path is required")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if len(data) == 0 {
		return errors.New("file is empty")
	}

	target, err := client.FindContactTarget(ctx, recipient)
	if err != nil {
		return err
	}
	jid := target.PrimaryJID

	ext := strings.ToLower(filepath.Ext(filePath))
	mimetype := mime.TypeByExtension(ext)
	if mimetype == "" {
		switch ext {
		case ".jpg", ".jpeg":
			mimetype = "image/jpeg"
		case ".png":
			mimetype = "image/png"
		case ".gif":
			mimetype = "image/gif"
		case ".webp":
			mimetype = "image/webp"
		case ".mp4":
			mimetype = "video/mp4"
		case ".mov":
			mimetype = "video/quicktime"
		case ".mkv":
			mimetype = "video/x-matroska"
		case ".3gp":
			mimetype = "video/3gpp"
		case ".mp3":
			mimetype = "audio/mpeg"
		case ".ogg", ".opus":
			mimetype = "audio/ogg"
		case ".m4a":
			mimetype = "audio/mp4"
		case ".wav":
			mimetype = "audio/wav"
		case ".pdf":
			mimetype = "application/pdf"
		default:
			mimetype = "application/octet-stream"
		}
	}

	var (
		mediaType    whatsmeow.MediaType
		waMsg        *waE2E.Message
		recordedText string
	)
	fileName := filepath.Base(filePath)

	if strings.HasPrefix(mimetype, "image/") && mimetype != "image/svg+xml" {
		mediaType = whatsmeow.MediaImage
	} else if strings.HasPrefix(mimetype, "video/") {
		mediaType = whatsmeow.MediaVideo
	} else if strings.HasPrefix(mimetype, "audio/") {
		mediaType = whatsmeow.MediaAudio
	} else {
		mediaType = whatsmeow.MediaDocument
	}

	response, err := client.device.Upload(ctx, data, mediaType)
	if err != nil {
		return fmt.Errorf("upload media (%s): %w", mediaType, err)
	}

	switch mediaType {
	case whatsmeow.MediaImage:
		img := &waE2E.ImageMessage{
			URL:           &response.URL,
			DirectPath:    &response.DirectPath,
			Mimetype:      proto.String(mimetype),
			FileSHA256:    response.FileSHA256,
			FileEncSHA256: response.FileEncSHA256,
			MediaKey:      response.MediaKey,
			FileLength:    &response.FileLength,
		}
		if caption != "" {
			img.Caption = proto.String(caption)
			recordedText = fmt.Sprintf("[Photo] %s", caption)
		} else {
			recordedText = "[Photo]"
		}
		waMsg = &waE2E.Message{ImageMessage: img}

	case whatsmeow.MediaVideo:
		vid := &waE2E.VideoMessage{
			URL:           &response.URL,
			DirectPath:    &response.DirectPath,
			Mimetype:      proto.String(mimetype),
			FileSHA256:    response.FileSHA256,
			FileEncSHA256: response.FileEncSHA256,
			MediaKey:      response.MediaKey,
			FileLength:    &response.FileLength,
		}
		if caption != "" {
			vid.Caption = proto.String(caption)
			recordedText = fmt.Sprintf("[Video] %s", caption)
		} else {
			recordedText = "[Video]"
		}
		waMsg = &waE2E.Message{VideoMessage: vid}

	case whatsmeow.MediaAudio:
		aud := &waE2E.AudioMessage{
			URL:           &response.URL,
			DirectPath:    &response.DirectPath,
			Mimetype:      proto.String(mimetype),
			FileSHA256:    response.FileSHA256,
			FileEncSHA256: response.FileEncSHA256,
			MediaKey:      response.MediaKey,
			FileLength:    &response.FileLength,
		}
		recordedText = "[Audio]"
		waMsg = &waE2E.Message{AudioMessage: aud}

	default:
		doc := &waE2E.DocumentMessage{
			URL:           &response.URL,
			DirectPath:    &response.DirectPath,
			Mimetype:      proto.String(mimetype),
			Title:         proto.String(fileName),
			FileName:      proto.String(fileName),
			FileSHA256:    response.FileSHA256,
			FileEncSHA256: response.FileEncSHA256,
			MediaKey:      response.MediaKey,
			FileLength:    &response.FileLength,
		}
		if caption != "" {
			doc.Caption = proto.String(caption)
			recordedText = fmt.Sprintf("[%s] %s", fileName, caption)
		} else {
			recordedText = fmt.Sprintf("[%s]", fileName)
		}
		waMsg = &waE2E.Message{DocumentMessage: doc}
	}

	info, err := client.device.SendMessage(ctx, jid, waMsg)
	if err != nil {
		return fmt.Errorf("send media message: %w", err)
	}

	sender := types.JID{}
	if client.device.Store.ID != nil {
		sender = client.device.Store.ID.ToNonAD()
	}
	client.record(Message{ID: info.ID, Chat: jid, Sender: sender, Text: recordedText, At: time.Now(), FromMe: true})
	return nil
}

func (client *Client) SendFile(ctx context.Context, recipient, path string) error {
	return client.SendMedia(ctx, recipient, path, "")
}

func (client *Client) Reply(ctx context.Context, recipient, text string, index int) error {
	target, err := client.FindContactTarget(ctx, recipient)
	if err != nil {
		return err
	}
	jid := target.PrimaryJID
	messages := client.MessagesForTarget(target)
	var quoted *Message
	for i := range messages {
		if index == 0 || messages[i].Index == index {
			copyMsg := messages[i]
			quoted = &copyMsg
		}
	}
	if quoted == nil {
		return errors.New("message reference not found; run inspect first")
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
	sender := types.JID{}
	if client.device.Store.ID != nil {
		sender = client.device.Store.ID.ToNonAD()
	}
	client.record(Message{ID: info.ID, Chat: jid, Sender: sender, Text: text, At: time.Now(), FromMe: true})
	return nil
}

func (client *Client) React(ctx context.Context, recipient string, index int, reaction string) error {
	target, err := client.FindContactTarget(ctx, recipient)
	if err != nil {
		return err
	}
	jid := target.PrimaryJID
	var quoted *Message
	for _, message := range client.MessagesForTarget(target) {
		if message.Index == index {
			copyMsg := message
			quoted = &copyMsg
			break
		}
	}
	if quoted == nil {
		return errors.New("message reference not found; run inspect first")
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
