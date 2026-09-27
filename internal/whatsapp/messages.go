package whatsapp

import (
	"context"
	"fmt"
	"sort"
	"time"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	waHistorySync "go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
)

type Message struct {
	Index  int       `json:"Index"`
	ID     string    `json:"ID"`
	Chat   types.JID `json:"Chat"`
	Sender types.JID `json:"Sender"`
	Text   string    `json:"Text"`
	At     time.Time `json:"At"`
	FromMe bool      `json:"FromMe"`
}

func (client *Client) Messages() []Message {
	client.mu.RLock()
	defer client.mu.RUnlock()
	result := make([]Message, len(client.messages))
	copy(result, client.messages)
	return result
}

func (client *Client) MessagesForTarget(target *ContactTarget) []Message {
	client.mu.RLock()
	defer client.mu.RUnlock()

	relatedSet := make(map[string]bool, len(target.RelatedJIDs))
	for _, j := range target.RelatedJIDs {
		relatedSet[j.ToNonAD().String()] = true
		if j.User != "" {
			relatedSet[j.User] = true
		}
	}

	seenIDs := make(map[string]bool)
	var matched []Message

	for _, msg := range client.messages {
		if msg.Text == "" {
			continue
		}
		chatNonAD := msg.Chat.ToNonAD().String()
		senderNonAD := msg.Sender.ToNonAD().String()

		isMatch := relatedSet[chatNonAD] || relatedSet[msg.Chat.User] ||
			(!msg.FromMe && (relatedSet[senderNonAD] || relatedSet[msg.Sender.User]))

		if isMatch {
			if !seenIDs[msg.ID] {
				seenIDs[msg.ID] = true
				matched = append(matched, msg)
			}
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].At.Before(matched[j].At)
	})

	return matched
}

func (client *Client) RequestHistory(ctx context.Context, jid types.JID, count int) error {
	messages := client.Messages()
	var oldest *Message
	jidNonAD := jid.ToNonAD()

	for i := range messages {
		if messages[i].Chat.ToNonAD() == jidNonAD && (oldest == nil || messages[i].At.Before(oldest.At)) {
			copyMsg := messages[i]
			oldest = &copyMsg
		}
	}
	if oldest == nil {
		return nil
	}
	info := &types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, IsFromMe: oldest.FromMe, Sender: oldest.Sender}, ID: types.MessageID(oldest.ID), Timestamp: oldest.At}
	request := client.device.BuildHistorySyncRequest(info, count)
	if _, err := client.device.SendPeerMessage(ctx, request); err != nil {
		return fmt.Errorf("request chat history: %w", err)
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-client.historyDone:
		return nil
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (client *Client) recordHistorySync(history *waHistorySync.HistorySync) {
	for _, conversation := range history.GetConversations() {
		chat, err := types.ParseJID(conversation.GetID())
		if err != nil || chat.IsEmpty() {
			continue
		}
		for _, historical := range conversation.GetMessages() {
			info := historical.GetMessage()
			if info == nil || info.GetKey() == nil || info.GetMessage() == nil {
				continue
			}
			key := info.GetKey()
			sender := chat
			if key.GetParticipant() != "" {
				sender, _ = types.ParseJID(key.GetParticipant())
			} else if key.GetFromMe() && client.device.Store.ID != nil {
				sender = client.device.Store.ID.ToNonAD()
			}
			text := ExtractMessageText(info.GetMessage())
			if text == "" {
				continue
			}
			ts := time.Unix(int64(info.GetMessageTimestamp()), 0)
			client.recordInMemory(Message{
				ID:     key.GetID(),
				Chat:   chat,
				Sender: sender,
				Text:   text,
				At:     ts,
				FromMe: key.GetFromMe(),
			})
		}
	}
	client.scheduleSave()
}

func ExtractMessageText(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Conversation != nil && *msg.Conversation != "" {
		return *msg.Conversation
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil && ext.GetText() != "" {
		return ext.GetText()
	}
	if eph := msg.GetEphemeralMessage(); eph != nil && eph.GetMessage() != nil {
		return ExtractMessageText(eph.GetMessage())
	}
	if vo := msg.GetViewOnceMessage(); vo != nil && vo.GetMessage() != nil {
		return ExtractMessageText(vo.GetMessage())
	}
	if vo2 := msg.GetViewOnceMessageV2(); vo2 != nil && vo2.GetMessage() != nil {
		return ExtractMessageText(vo2.GetMessage())
	}
	if docCap := msg.GetDocumentWithCaptionMessage(); docCap != nil && docCap.GetMessage() != nil {
		return ExtractMessageText(docCap.GetMessage())
	}
	if edited := msg.GetEditedMessage(); edited != nil && edited.GetMessage() != nil {
		return ExtractMessageText(edited.GetMessage())
	}
	if protoMsg := msg.GetProtocolMessage(); protoMsg != nil && protoMsg.GetEditedMessage() != nil {
		return ExtractMessageText(protoMsg.GetEditedMessage())
	}
	if img := msg.GetImageMessage(); img != nil {
		if img.GetCaption() != "" {
			return img.GetCaption()
		}
		return "[Photo]"
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		if vid.GetCaption() != "" {
			return vid.GetCaption()
		}
		return "[Video]"
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		name := doc.GetFileName()
		if name == "" {
			name = "Document"
		}
		if doc.GetCaption() != "" {
			return fmt.Sprintf("[%s] %s", name, doc.GetCaption())
		}
		return fmt.Sprintf("[%s]", name)
	}
	if aud := msg.GetAudioMessage(); aud != nil {
		if aud.GetPTT() {
			return "[Voice message]"
		}
		return "[Audio]"
	}
	if stk := msg.GetStickerMessage(); stk != nil {
		return "[Sticker]"
	}
	if loc := msg.GetLocationMessage(); loc != nil {
		if loc.GetName() != "" {
			return fmt.Sprintf("[Location: %s]", loc.GetName())
		}
		return "[Location]"
	}
	if contact := msg.GetContactMessage(); contact != nil {
		if contact.GetDisplayName() != "" {
			return fmt.Sprintf("[Contact: %s]", contact.GetDisplayName())
		}
		return "[Contact]"
	}
	if poll := msg.GetPollCreationMessage(); poll != nil {
		return fmt.Sprintf("[Poll: %s]", poll.GetName())
	}
	if poll := msg.GetPollCreationMessageV2(); poll != nil {
		return fmt.Sprintf("[Poll: %s]", poll.GetName())
	}
	if poll := msg.GetPollCreationMessageV3(); poll != nil {
		return fmt.Sprintf("[Poll: %s]", poll.GetName())
	}
	if tpl := msg.GetTemplateMessage(); tpl != nil {
		if hyd := tpl.GetHydratedTemplate(); hyd != nil {
			return hyd.GetHydratedContentText()
		}
		if hyd4 := tpl.GetHydratedFourRowTemplate(); hyd4 != nil {
			return hyd4.GetHydratedContentText()
		}
	}
	return ""
}
