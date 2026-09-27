package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

type ContactTarget struct {
	PrimaryJID  types.JID
	RelatedJIDs []types.JID
	DisplayName string
	PhoneNumber string
}

func (target *ContactTarget) addRelated(jid types.JID) {
	if jid.IsEmpty() {
		return
	}
	clean := jid.ToNonAD()
	for _, existing := range target.RelatedJIDs {
		if existing == clean {
			return
		}
	}
	target.RelatedJIDs = append(target.RelatedJIDs, clean)
}

func (client *Client) FindContactTarget(ctx context.Context, input string) (*ContactTarget, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("contact name or phone number is required")
	}

	target := &ContactTarget{
		RelatedJIDs: make([]types.JID, 0),
	}

	// 1. Check if input is direct JID
	if jid, err := types.ParseJID(input); err == nil && !jid.IsEmpty() && isWhatsAppServer(jid.Server) {
		target.PrimaryJID = jid.ToNonAD()
		target.addRelated(jid.ToNonAD())
		if alt, err := client.device.Store.GetAltJID(ctx, jid); err == nil && !alt.IsEmpty() {
			target.addRelated(alt.ToNonAD())
		}
		target.DisplayName = client.getContactNameOrFormatted(ctx, target.PrimaryJID)
		return target, nil
	}

	phone := client.normalizePhoneForAccount(input)
	contacts, _ := client.device.Store.Contacts.GetAllContacts(ctx)

	// 2. Check if input is a digit-based phone number
	if isDigits(phone) {
		target.PhoneNumber = phone
		for jid, contact := range contacts {
			if jid.Server == "s.whatsapp.net" && jid.User == phone {
				target.PrimaryJID = jid.ToNonAD()
				target.addRelated(jid.ToNonAD())
				target.DisplayName = contactDisplayName(contact)
				if alt, err := client.device.Store.GetAltJID(ctx, jid); err == nil && !alt.IsEmpty() {
					target.addRelated(alt.ToNonAD())
				}
				break
			}
		}

		if target.PrimaryJID.IsEmpty() {
			whatsappContacts, err := client.device.IsOnWhatsApp(ctx, []string{"+" + phone})
			if err == nil && len(whatsappContacts) > 0 && whatsappContacts[0].IsIn && !whatsappContacts[0].JID.IsEmpty() {
				target.PrimaryJID = whatsappContacts[0].JID.ToNonAD()
				target.addRelated(target.PrimaryJID)
				if alt, err := client.device.Store.GetAltJID(ctx, target.PrimaryJID); err == nil && !alt.IsEmpty() {
					target.addRelated(alt.ToNonAD())
				}
				target.DisplayName = "+" + phone
			}
		}

		if !target.PrimaryJID.IsEmpty() {
			client.collectMatchingNameJIDs(ctx, target, contacts)
			return target, nil
		}
	}

	// 3. Search contacts by name with ranked fuzzy/substring matching
	type match struct {
		jid     types.JID
		contact types.ContactInfo
		score   int
	}

	var matches []match
	queryLower := strings.ToLower(input)
	queryWords := strings.Fields(queryLower)

	for jid, contact := range contacts {
		score := 0
		fullLower := strings.ToLower(contact.FullName)
		firstLower := strings.ToLower(contact.FirstName)
		pushLower := strings.ToLower(contact.PushName)
		bizLower := strings.ToLower(contact.BusinessName)

		if fullLower == queryLower || firstLower == queryLower || pushLower == queryLower || bizLower == queryLower {
			score = 100
		} else if strings.HasPrefix(fullLower, queryLower) || strings.HasPrefix(pushLower, queryLower) {
			score = 90
		} else {
			allFound := true
			for _, w := range queryWords {
				if !strings.Contains(fullLower, w) && !strings.Contains(firstLower, w) && !strings.Contains(pushLower, w) && !strings.Contains(bizLower, w) {
					allFound = false
					break
				}
			}
			if allFound && len(queryWords) > 0 {
				score = 80
			} else if strings.Contains(fullLower, queryLower) || strings.Contains(pushLower, queryLower) {
				score = 70
			}
		}

		if score > 0 {
			matches = append(matches, match{jid: jid, contact: contact, score: score})
		}
	}

	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].score > matches[j].score
		})

		best := matches[0]
		target.DisplayName = contactDisplayName(best.contact)
		target.PrimaryJID = best.jid.ToNonAD()
		target.addRelated(best.jid.ToNonAD())

		for _, m := range matches {
			if m.score >= 70 {
				target.addRelated(m.jid.ToNonAD())
				if alt, err := client.device.Store.GetAltJID(ctx, m.jid); err == nil && !alt.IsEmpty() {
					target.addRelated(alt.ToNonAD())
				}
				if target.DisplayName == "" {
					target.DisplayName = contactDisplayName(m.contact)
				}
			}
		}

		if alt, err := client.device.Store.GetAltJID(ctx, target.PrimaryJID); err == nil && !alt.IsEmpty() {
			target.addRelated(alt.ToNonAD())
		}

		client.collectMatchingNameJIDs(ctx, target, contacts)
		return target, nil
	}

	// 4. Check joined groups
	groups, err := client.device.GetJoinedGroups(ctx)
	if err == nil {
		for _, group := range groups {
			if strings.EqualFold(input, group.Name) || strings.Contains(strings.ToLower(group.Name), queryLower) {
				target.PrimaryJID = group.JID.ToNonAD()
				target.addRelated(group.JID.ToNonAD())
				target.DisplayName = group.Name
				return target, nil
			}
		}
	}

	return nil, fmt.Errorf("contact not found: %s", input)
}

func (client *Client) collectMatchingNameJIDs(ctx context.Context, target *ContactTarget, contacts map[types.JID]types.ContactInfo) {
	if target.DisplayName == "" {
		return
	}
	nameWords := strings.Fields(strings.ToLower(target.DisplayName))
	for jid, contact := range contacts {
		name := strings.ToLower(contactDisplayName(contact))
		if name == "" {
			continue
		}
		match := true
		for _, w := range nameWords {
			if len(w) > 2 && !strings.Contains(name, w) {
				match = false
				break
			}
		}
		if match && len(nameWords) > 0 {
			target.addRelated(jid.ToNonAD())
			if alt, err := client.device.Store.GetAltJID(ctx, jid); err == nil && !alt.IsEmpty() {
				target.addRelated(alt.ToNonAD())
			}
		}
	}
}

func contactDisplayName(c types.ContactInfo) string {
	if c.FullName != "" {
		return c.FullName
	}
	if c.FirstName != "" {
		return c.FirstName
	}
	if c.PushName != "" {
		return c.PushName
	}
	if c.BusinessName != "" {
		return c.BusinessName
	}
	return ""
}

func (client *Client) getContactNameOrFormatted(ctx context.Context, jid types.JID) string {
	contacts, err := client.device.Store.Contacts.GetAllContacts(ctx)
	if err == nil {
		if c, ok := contacts[jid]; ok {
			name := contactDisplayName(c)
			if name != "" {
				return name
			}
		}
		if alt, err := client.device.Store.GetAltJID(ctx, jid); err == nil && !alt.IsEmpty() {
			if c, ok := contacts[alt]; ok {
				name := contactDisplayName(c)
				if name != "" {
					return name
				}
			}
		}
	}
	if jid.Server == "s.whatsapp.net" {
		return "+" + jid.User
	}
	return jid.String()
}

func (client *Client) ResolveRecipient(ctx context.Context, input string) (types.JID, error) {
	target, err := client.FindContactTarget(ctx, input)
	if err != nil {
		return types.JID{}, err
	}
	return target.PrimaryJID, nil
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
	return server == "s.whatsapp.net" || server == "g.us" || server == "broadcast" || server == "lid"
}
