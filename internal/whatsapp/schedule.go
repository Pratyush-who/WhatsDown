package whatsapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ScheduleStatus string

const (
	ScheduleStatusPending   ScheduleStatus = "pending"
	ScheduleStatusSent      ScheduleStatus = "sent"
	ScheduleStatusFailed    ScheduleStatus = "failed"
	ScheduleStatusCancelled ScheduleStatus = "cancelled"
)

type ScheduledMessage struct {
	ID          string         `json:"id"`
	Recipient   string         `json:"recipient"`
	Text        string         `json:"text,omitempty"`
	MediaPath   string         `json:"media_path,omitempty"`
	Caption     string         `json:"caption,omitempty"`
	ScheduledAt time.Time      `json:"scheduled_at"`
	CreatedAt   time.Time      `json:"created_at"`
	Status      ScheduleStatus `json:"status"`
	Error       string         `json:"error,omitempty"`
	SentAt      *time.Time     `json:"sent_at,omitempty"`
}

type ScheduleStore struct {
	filePath string
	mu       sync.RWMutex
	items    []ScheduledMessage
}

func NewScheduleStore(dataDir string) (*ScheduleStore, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	store := &ScheduleStore{
		filePath: filepath.Join(dataDir, "schedules.json"),
		items:    make([]ScheduledMessage, 0),
	}

	if data, err := os.ReadFile(store.filePath); err == nil {
		_ = json.Unmarshal(data, &store.items)
	}

	return store, nil
}

func (s *ScheduleStore) save() error {
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := s.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.filePath)
}

func generateScheduleID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("sch_%d_%s", time.Now().Unix()%100000, hex.EncodeToString(b))
}

func (s *ScheduleStore) Add(recipient, text, mediaPath, caption string, scheduledAt time.Time) (*ScheduledMessage, error) {
	if recipient == "" {
		return nil, errors.New("recipient is required")
	}
	if text == "" && mediaPath == "" {
		return nil, errors.New("message text or media file is required")
	}
	if scheduledAt.Before(time.Now().Add(-1 * time.Minute)) {
		return nil, errors.New("scheduled time must be in the future")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item := ScheduledMessage{
		ID:          generateScheduleID(),
		Recipient:   recipient,
		Text:        text,
		MediaPath:   mediaPath,
		Caption:     caption,
		ScheduledAt: scheduledAt,
		CreatedAt:   time.Now(),
		Status:      ScheduleStatusPending,
	}

	s.items = append(s.items, item)
	if err := s.save(); err != nil {
		return nil, fmt.Errorf("save schedule: %w", err)
	}
	return &item, nil
}

func (s *ScheduleStore) List(includeAll bool) []ScheduledMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ScheduledMessage
	for _, item := range s.items {
		if includeAll || item.Status == ScheduleStatusPending {
			result = append(result, item)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ScheduledAt.Before(result[j].ScheduledAt)
	})
	return result
}

func (s *ScheduleStore) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	for i := range s.items {
		if s.items[i].ID == id {
			if s.items[i].Status != ScheduleStatusPending {
				return fmt.Errorf("schedule %s is already %s", id, s.items[i].Status)
			}
			s.items[i].Status = ScheduleStatusCancelled
			return s.save()
		}
	}
	return fmt.Errorf("schedule %s not found", id)
}

func (s *ScheduleStore) MarkSent(id string, sentAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Status = ScheduleStatusSent
			s.items[i].SentAt = &sentAt
			s.items[i].Error = ""
			return s.save()
		}
	}
	return nil
}

func (s *ScheduleStore) MarkFailed(id string, sendErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].Status = ScheduleStatusFailed
			if sendErr != nil {
				s.items[i].Error = sendErr.Error()
			}
			return s.save()
		}
	}
	return nil
}

func (s *ScheduleStore) GetDuePending(now time.Time) []ScheduledMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var due []ScheduledMessage
	for _, item := range s.items {
		if item.Status == ScheduleStatusPending && (item.ScheduledAt.Before(now) || item.ScheduledAt.Equal(now)) {
			due = append(due, item)
		}
	}
	return due
}

func (client *Client) ProcessSchedules(ctx context.Context) (int, error) {
	if client.schedules == nil {
		return 0, nil
	}

	due := client.schedules.GetDuePending(time.Now())
	if len(due) == 0 {
		return 0, nil
	}

	processed := 0
	for _, item := range due {
		var err error
		if item.MediaPath != "" {
			err = client.SendMedia(ctx, item.Recipient, item.MediaPath, item.Caption)
		} else {
			err = client.SendText(ctx, item.Recipient, item.Text)
		}

		if err != nil {
			_ = client.schedules.MarkFailed(item.ID, err)
			fmt.Printf("[Scheduler] Failed sending to %s: %v\n", item.Recipient, err)
		} else {
			_ = client.schedules.MarkSent(item.ID, time.Now())
			processed++
			fmt.Printf("[Scheduler] Successfully sent scheduled message to %s (ID: %s)\n", item.Recipient, item.ID)
		}
	}

	return processed, nil
}

func (client *Client) Schedules() *ScheduleStore {
	return client.schedules
}

// ParseScheduleTime parses flexible date and time strings:
// - "15-10-2026 00:00", "15-10-2026 14:30:00"
// - "2026-10-15 00:00", "2026-10-15 14:30:00"
// - "15/10/2026 00:00", "2026/10/15 00:00"
// - "15-10-2026", "2026-10-15" (defaults to 00:00)
// - "15:04", "15:04:05" (today, or tomorrow if time has passed)
// - "in 10m", "in 2h", "in 1d", "in 30s", "10m", "2h", "1d"
// - "tomorrow 09:00", "tomorrow 15:30", "today 18:00"
func ParseScheduleTime(input string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	cleaned := strings.TrimSpace(input)
	if cleaned == "" {
		return time.Time{}, errors.New("scheduled time is required")
	}

	lower := strings.ToLower(cleaned)

	if strings.HasPrefix(lower, "in ") {
		lower = strings.TrimSpace(strings.TrimPrefix(lower, "in "))
	}

	if d, err := parseRelativeDuration(lower); err == nil {
		return now.Add(d), nil
	}

	if strings.HasPrefix(lower, "tomorrow") {
		rest := strings.TrimSpace(strings.TrimPrefix(lower, "tomorrow"))
		tomorrow := now.AddDate(0, 0, 1)
		if rest == "" {
			return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 9, 0, 0, 0, loc), nil
		}
		t, err := parseClockTime(rest, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid time format after tomorrow: %w", err)
		}
		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
	}

	if strings.HasPrefix(lower, "today") {
		rest := strings.TrimSpace(strings.TrimPrefix(lower, "today"))
		if rest == "" {
			return time.Time{}, errors.New("time of day is required for 'today', e.g. 'today 18:00'")
		}
		t, err := parseClockTime(rest, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid time format after today: %w", err)
		}
		target := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc)
		if target.Before(now) {
			return time.Time{}, errors.New("scheduled time today is already in the past")
		}
		return target, nil
	}

	if t, err := parseClockTime(cleaned, loc); err == nil {
		target := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc)
		if target.Before(now) {
			target = target.AddDate(0, 0, 1)
		}
		return target, nil
	}
	layouts := []string{
		"02-01-2006 15:04:05",
		"02-01-2006 15:04",
		"02/01/2006 15:04:05",
		"02/01/2006 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"02-01-2006",
		"02/01/2006",
		"2006-01-02",
		"2006/01/02",
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, cleaned, loc); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse date/time %q. Examples: '15-10-2026 00:00', '2026-10-15 14:30', 'in 2h', 'tomorrow 09:00'", input)
}

func parseClockTime(input string, loc *time.Location) (time.Time, error) {
	timeLayouts := []string{
		"15:04:05",
		"15:04",
		"3:04PM",
		"3:04pm",
		"3PM",
		"3pm",
	}
	inputClean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(input), " ", ""))
	for _, l := range timeLayouts {
		if t, err := time.ParseInLocation(l, inputClean, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid clock format")
}

var durationRegex = regexp.MustCompile(`^(\d+)\s*(s|sec|seconds?|m|min|minutes?|h|hr|hours?|d|days?|w|weeks?)$`)

func parseRelativeDuration(input string) (time.Duration, error) {
	input = strings.TrimSpace(input)
	matches := durationRegex.FindStringSubmatch(input)
	if len(matches) < 3 {
		return 0, errors.New("not a relative duration")
	}

	val, err := strconv.Atoi(matches[1])
	if err != nil || val <= 0 {
		return 0, errors.New("invalid duration number")
	}

	unit := matches[2]
	switch {
	case strings.HasPrefix(unit, "s"):
		return time.Duration(val) * time.Second, nil
	case strings.HasPrefix(unit, "m"):
		return time.Duration(val) * time.Minute, nil
	case strings.HasPrefix(unit, "h"):
		return time.Duration(val) * time.Hour, nil
	case strings.HasPrefix(unit, "d"):
		return time.Duration(val) * 24 * time.Hour, nil
	case strings.HasPrefix(unit, "w"):
		return time.Duration(val) * 7 * 24 * time.Hour, nil
	}

	return 0, errors.New("unknown time unit")
}
