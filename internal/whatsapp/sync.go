package whatsapp

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

var essentialPatches = []appstate.WAPatchName{
	appstate.WAPatchCriticalBlock,
	appstate.WAPatchCriticalUnblockLow,
	appstate.WAPatchRegularHigh,
}

type SyncProgress struct {
	StageIndex  int    // Current stage number, e.g. 1
	StageTotal  int    // Total stages, e.g. 3
	StageName   string // e.g. "Syncing App State", "Discovering Chats", "Syncing Chat History"
	Description string // e.g. patch name, contact name, or chat JID
	Current     int    // Current item within this stage
	Total       int    // Total items in this stage
	Done        bool   // Whether sync is finished
}

type SyncProgressCallback func(progress SyncProgress)

type SyncStatusInfo struct {
	IsRunning   bool
	StartTime   time.Time
	LastSync    time.Time
	LastError   error
	Progress    SyncProgress
	TargetName  string
	SyncedCount int
}

func (client *Client) IsSyncing() bool {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.syncRunning
}

func (client *Client) SyncStatus() SyncStatusInfo {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return SyncStatusInfo{
		IsRunning:   client.syncRunning,
		StartTime:   client.syncStartTime,
		LastSync:    client.lastSyncTime,
		LastError:   client.lastSyncError,
		Progress:    client.syncProgress,
		TargetName:  client.lastSyncTarget,
		SyncedCount: client.lastSyncCount,
	}
}

func (client *Client) StartBackgroundSync(ctx context.Context, target string, onProgress SyncProgressCallback, onDone func(synced int, err error)) (bool, error) {
	client.mu.Lock()
	if client.syncRunning {
		client.mu.Unlock()
		return false, errors.New("a sync is already running")
	}
	client.syncRunning = true
	client.syncStartTime = time.Now()
	client.lastSyncTarget = target
	client.syncProgress = SyncProgress{
		StageIndex:  1,
		StageTotal:  3,
		StageName:   "Starting sync",
		Description: "Initializing...",
	}
	client.mu.Unlock()

	go func() {
		var count int
		var err error

		progressHandler := func(p SyncProgress) {
			client.mu.Lock()
			client.syncProgress = p
			client.mu.Unlock()
			if onProgress != nil {
				onProgress(p)
			}
		}

		if target != "" {
			var targetContact *ContactTarget
			targetContact, err = client.FindContactTarget(ctx, target)
			if err == nil {
				_ = client.SyncAppStateWithProgress(ctx, false, progressHandler)
				err = client.SyncChatWithProgress(ctx, targetContact.PrimaryJID, 25, progressHandler)
				if err == nil {
					count = 1
				}
			}
		} else {
			count, err = client.SyncAllWithProgress(ctx, progressHandler)
		}

		client.mu.Lock()
		client.syncRunning = false
		client.lastSyncTime = time.Now()
		client.lastSyncError = err
		client.lastSyncCount = count
		client.mu.Unlock()

		if onDone != nil {
			onDone(count, err)
		}
	}()

	return true, nil
}

func (client *Client) SyncAppState(ctx context.Context, fullSync bool) error {
	return client.SyncAppStateWithProgress(ctx, fullSync, nil)
}

func (client *Client) SyncAppStateWithProgress(ctx context.Context, fullSync bool, cb SyncProgressCallback) error {
	total := len(essentialPatches)
	for i, patchName := range essentialPatches {
		if cb != nil {
			cb(SyncProgress{
				StageIndex:  1,
				StageTotal:  3,
				StageName:   "App State",
				Description: string(patchName),
				Current:     i + 1,
				Total:       total,
			})
		}
		_ = client.device.FetchAppState(ctx, patchName, fullSync, false)
	}
	_, _ = client.device.GetJoinedGroups(ctx)
	return nil
}

func (client *Client) SyncChat(ctx context.Context, jid types.JID, count int) error {
	return client.SyncChatWithProgress(ctx, jid, count, nil)
}

func (client *Client) SyncChatWithProgress(ctx context.Context, jid types.JID, count int, cb SyncProgressCallback) error {
	if count <= 0 {
		count = 25
	}
	name := client.getContactNameOrFormatted(ctx, jid)
	if cb != nil {
		cb(SyncProgress{
			StageIndex:  1,
			StageTotal:  1,
			StageName:   "Chat History",
			Description: name,
			Current:     1,
			Total:       1,
		})
	}
	return client.RequestRecentHistory(ctx, jid, count)
}

func (client *Client) SyncAll(ctx context.Context) (int, error) {
	return client.SyncAllWithProgress(ctx, nil)
}

func (client *Client) SyncAllWithProgress(ctx context.Context, cb SyncProgressCallback) (int, error) {
	// Stage 1: Optimized App State Sync (essential patches only)
	if err := client.SyncAppStateWithProgress(ctx, false, cb); err != nil {
		return 0, err
	}

	// Stage 2: Discovering Recent Active Chats & Groups
	if cb != nil {
		cb(SyncProgress{
			StageIndex:  2,
			StageTotal:  3,
			StageName:   "Discovering",
			Description: "Scanning recent active chats...",
			Current:     0,
			Total:       1,
		})
	}

	cutoff := time.Now().AddDate(0, 0, -SyncHistoryDays)
	messages := client.Messages()
	seen := map[string]types.JID{}

	// Select top active chats within the cutoff window
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if !msg.At.IsZero() && msg.At.Before(cutoff) {
			continue // Skip chats whose last activity is before cutoff
		}
		chatKey := msg.Chat.ToNonAD().String()
		if _, exists := seen[chatKey]; !exists {
			seen[chatKey] = msg.Chat.ToNonAD()
		}
		if len(seen) >= 8 {
			break
		}
	}

	// Fallback to top recent chats if no chats matched the window
	if len(seen) == 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			chatKey := messages[i].Chat.ToNonAD().String()
			if _, exists := seen[chatKey]; !exists {
				seen[chatKey] = messages[i].Chat.ToNonAD()
			}
			if len(seen) >= 5 {
				break
			}
		}
	}

	// Add recent joined groups (max 3)
	if groups, err := client.device.GetJoinedGroups(ctx); err == nil {
		addedGroups := 0
		for _, group := range groups {
			if addedGroups >= 3 {
				break
			}
			chatKey := group.JID.ToNonAD().String()
			if _, exists := seen[chatKey]; !exists {
				seen[chatKey] = group.JID.ToNonAD()
				addedGroups++
			}
		}
	}

	chatList := make([]types.JID, 0, len(seen))
	for _, jid := range seen {
		chatList = append(chatList, jid)
	}
	totalChats := len(chatList)

	// Stage 3: Fetching Chat Histories for Active Chats
	synced := 0
	for i, jid := range chatList {
		chatName := client.getContactNameOrFormatted(ctx, jid)
		if cb != nil {
			cb(SyncProgress{
				StageIndex:  3,
				StageTotal:  3,
				StageName:   "Syncing Chats",
				Description: chatName,
				Current:     i + 1,
				Total:       totalChats,
			})
		}
		chatCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		_ = client.RequestRecentHistory(chatCtx, jid, 20)
		cancel()
		synced++
	}

	if cb != nil {
		cb(SyncProgress{
			StageIndex:  3,
			StageTotal:  3,
			StageName:   "Syncing Chats",
			Description: "Completed",
			Current:     synced,
			Total:       totalChats,
			Done:        true,
		})
	}

	return synced, nil
}
