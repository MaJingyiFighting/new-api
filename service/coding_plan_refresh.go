package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

const (
	// CodingPlanQuotaRefreshInterval is how often the background task probes
	// all Coding Plan channels for their current subscription usage.
	CodingPlanQuotaRefreshInterval = 10 * time.Minute

	// codingPlanRefreshConcurrency limits how many channels are probed at once.
	codingPlanRefreshConcurrency = 8
)

// CodingPlanQuotaRefreshTask periodically probes the quota for all channels
// that belong to one of the five Coding Plan providers (Kimi, Zhipu, MiniMax,
// Volcengine, MiMo). Results are written into ChannelInfo.CodingPlanQuota.
type CodingPlanQuotaRefreshTask struct {
	interval time.Duration
	mu       sync.Mutex
	stopCh   chan struct{}
}

// NewCodingPlanQuotaRefreshTask creates a new task with the default interval.
func NewCodingPlanQuotaRefreshTask() *CodingPlanQuotaRefreshTask {
	return &CodingPlanQuotaRefreshTask{
		interval: CodingPlanQuotaRefreshInterval,
	}
}

// Start begins the periodic refresh in a background goroutine.
func (t *CodingPlanQuotaRefreshTask) Start(ctx context.Context) {
	t.mu.Lock()
	if t.stopCh != nil {
		t.mu.Unlock()
		return // already running
	}
	stopCh := make(chan struct{})
	t.stopCh = stopCh
	t.mu.Unlock()

	go t.loop(ctx, stopCh)
}

// Stop signals the background loop to exit.
func (t *CodingPlanQuotaRefreshTask) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopCh != nil {
		close(t.stopCh)
		t.stopCh = nil
	}
}

// RunOnce executes one full refresh cycle synchronously. It probes every
// channel whose Type is a Coding Plan channel type (60-64) and writes the
// result into ChannelInfo.CodingPlanQuota. Errors from individual channels
// are logged but do not abort the cycle.
func (t *CodingPlanQuotaRefreshTask) RunOnce(ctx context.Context) error {
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return fmt.Errorf("failed to list channels: %w", err)
	}

	sem := make(chan struct{}, codingPlanRefreshConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for i := range channels {
		ch := channels[i]
		if !isCodingPlanChannelType(ch.Type) {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(c *model.Channel) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := refreshSingleChannelQuota(ctx, c); err != nil {
				common.SysLog(fmt.Sprintf("quota refresh failed for channel #%d: %v", c.Id, err))
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(ch)
	}
	wg.Wait()

	return firstErr
}

func (t *CodingPlanQuotaRefreshTask) loop(ctx context.Context, stopCh chan struct{}) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	// Run once immediately on start.
	_ = t.RunOnce(ctx)

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			_ = t.RunOnce(ctx)
		}
	}
}

// RefreshSingleChannelQuota probes and persists the Coding Plan quota for a
// single channel. This is the public entry point used by the HTTP handler and
// for on-demand refresh.
func RefreshSingleChannelQuota(ctx context.Context, channelID int) error {
	ch, err := model.CacheGetChannel(channelID)
	if err != nil {
		return fmt.Errorf("failed to get channel #%d: %w", channelID, err)
	}
	if ch == nil {
		return fmt.Errorf("channel #%d not found", channelID)
	}
	return refreshSingleChannelQuota(ctx, ch)
}

// refreshSingleChannelQuota is the internal implementation shared by
// RunOnce and RefreshSingleChannelQuota. It does nothing when the channel
// is not a Coding Plan type.
func refreshSingleChannelQuota(ctx context.Context, ch *model.Channel) error {
	if !isCodingPlanChannelType(ch.Type) {
		return nil
	}

	baseURL := CodingPlanBaseURL(ch)
	if baseURL == "" {
		return nil
	}

	apiKey := CodingPlanAPIKey(ch)
	if apiKey == "" {
		return nil
	}

	provider := DetectCodingPlanProviderFromBaseURL(baseURL)
	if provider == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	snapshot, err := ProbeCodingPlanQuota(ctx, baseURL, apiKey, provider)
	if err != nil && snapshot == nil {
		return fmt.Errorf("quota probe for %s failed: %w", provider, err)
	}

	// Convert the service snapshot to the model type.
	quota := convertToCodingPlanQuotaState(snapshot)

	info := &ch.ChannelInfo
	info.CodingPlanQuota = quota
	_ = ch.SaveChannelInfo()

	return nil
}

func isCodingPlanChannelType(channelType int) bool {
	for _, t := range constant.CodingPlanChannelTypes {
		if t == channelType {
			return true
		}
	}
	return false
}

func convertToCodingPlanQuotaState(snapshot *CodingPlanQuotaSnapshot) *model.CodingPlanQuotaState {
	if snapshot == nil {
		return nil
	}
	return &model.CodingPlanQuotaState{
		Provider:               snapshot.Provider,
		PlanName:               safeString(snapshot.PlanName),
		FiveHourUsedPercent:    snapshot.FiveHourUsedPercent,
		FiveHourResetAt:        snapshot.FiveHourResetAt,
		WeeklyUsedPercent:      snapshot.WeeklyUsedPercent,
		WeeklyResetAt:          snapshot.WeeklyResetAt,
		Success:                snapshot.Success,
		ErrorMessage:           snapshot.ErrorMessage,
		HTTPStatus:             snapshot.HTTPStatus,
		CredentialExpired:      snapshot.CredentialExpired,
		QuotaProbeStatus:       snapshot.QuotaProbeStatus,
		UpdatedAt:              snapshot.UpdatedAt,
	}
}

func safeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
