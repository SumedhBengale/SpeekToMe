package valkey

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	ChannelConfigReload    = "speektome:config:reload"
	ChannelVocabInvalidate = "speektome:vocab:invalidate"
	ChannelUserRPMReset    = "speektome:ratelimit:user:reset"

	KeyPrefixUserRPM      = "ratelimit:user:"
	KeyPrefixAuth         = "ratelimit:auth:"
	KeyPrefixGeoIP        = "geoip:"
	KeyPrefixSession      = "session:"
	KeyPrefixUserSessions = "user_sessions:"
	KeyPrefixVocab        = "vocab:user:"
	KeyPrefixSaaSConfig   = "config:saas"
	KeyModelsConfigJSON   = "config:models_config_json"
)

type Client struct {
	rdb       *redis.Client
	mu        sync.RWMutex
	connected bool
	cancelCtx context.CancelFunc
}

var (
	GlobalClient *Client
	once         sync.Once
)

// InitGlobalClient initializes the singleton Valkey / Redis client
func InitGlobalClient() *Client {
	once.Do(func() {
		GlobalClient = NewClientFromEnv()
	})
	return GlobalClient
}

// NewClientFromEnv instantiates a client using VALKEY_URL or REDIS_URL
func NewClientFromEnv() *Client {
	redisURL := os.Getenv("VALKEY_URL")
	if redisURL == "" {
		redisURL = os.Getenv("REDIS_URL")
	}
	if redisURL == "" {
		redisURL = "redis://127.0.0.1:6379/0"
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		// Try host:port fallback
		opts = &redis.Options{
			Addr:        redisURL,
			DialTimeout: 2 * time.Second,
			ReadTimeout: 2 * time.Second,
		}
	} else {
		opts.DialTimeout = 2 * time.Second
		opts.ReadTimeout = 2 * time.Second
	}

	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithCancel(context.Background())

	c := &Client{
		rdb:       rdb,
		cancelCtx: cancel,
	}

	// Test connection ping
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer pingCancel()

	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Printf("[VALKEY INFO] Server not reachable at %s (%v). Standalone memory fallback active.", opts.Addr, err)
		c.connected = false
	} else {
		log.Printf("[VALKEY SUCCESS] Connected to Valkey / Redis instance at %s", opts.Addr)
		c.connected = true
	}

	_ = ctx
	return c
}

func (c *Client) IsConnected() bool {
	if c == nil || c.rdb == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *Client) Raw() *redis.Client {
	if c == nil {
		return nil
	}
	return c.rdb
}

func (c *Client) Close() {
	if c != nil && c.cancelCtx != nil {
		c.cancelCtx()
	}
	if c != nil && c.rdb != nil {
		_ = c.rdb.Close()
	}
}

// ----------------------------------------------------------------------
// Rate Limiting Primitives (User RPM & Auth Brute Force)
// ----------------------------------------------------------------------

// AllowUserRPM checks and increments a user's rate limit counter atomically in Valkey.
// Returns (allowed, currentCount, error).
func (c *Client) AllowUserRPM(ctx context.Context, userID int64, limit int) (bool, int, error) {
	if !c.IsConnected() {
		return true, 0, fmt.Errorf("valkey disconnected")
	}

	key := fmt.Sprintf("%s%d", KeyPrefixUserRPM, userID)
	pipe := c.rdb.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, 60*time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return true, 0, err
	}

	count := int(incrCmd.Val())
	if count > limit {
		return false, count, nil
	}
	return true, count, nil
}

func (c *Client) ResetUserRPM(ctx context.Context, userID int64) error {
	if !c.IsConnected() {
		return nil
	}
	key := fmt.Sprintf("%s%d", KeyPrefixUserRPM, userID)
	return c.rdb.Del(ctx, key).Err()
}

func (c *Client) ResetAllUserRPM(ctx context.Context) error {
	if !c.IsConnected() {
		return nil
	}
	iter := c.rdb.Scan(ctx, 0, KeyPrefixUserRPM+"*", 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return c.rdb.Del(ctx, keys...).Err()
	}
	return nil
}

func (c *Client) GetUserRPMCounts(ctx context.Context) (map[int64]int, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("valkey disconnected")
	}

	result := make(map[int64]int)
	iter := c.rdb.Scan(ctx, 0, KeyPrefixUserRPM+"*", 0).Iterator()
	for iter.Next(ctx) {
		k := iter.Val()
		uIDStr := strings.TrimPrefix(k, KeyPrefixUserRPM)
		uID, err := strconv.ParseInt(uIDStr, 10, 64)
		if err != nil {
			continue
		}
		valStr, err := c.rdb.Get(ctx, k).Result()
		if err == nil {
			if count, err := strconv.Atoi(valStr); err == nil {
				result[uID] = count
			}
		}
	}
	return result, nil
}

// AllowAuthAttempt handles sliding / fixed window rate limiting for login/signup/2FA
func (c *Client) AllowAuthAttempt(ctx context.Context, identifier string, limit int, windowSec int) (bool, error) {
	if !c.IsConnected() {
		return true, fmt.Errorf("valkey disconnected")
	}

	key := fmt.Sprintf("%s%s", KeyPrefixAuth, identifier)
	pipe := c.rdb.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, time.Duration(windowSec)*time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return true, err
	}

	if int(incrCmd.Val()) > limit {
		return false, nil
	}
	return true, nil
}

// ----------------------------------------------------------------------
// GeoIP Cache Operations
// ----------------------------------------------------------------------

func (c *Client) GetGeoIP(ctx context.Context, ip string) (string, error) {
	if !c.IsConnected() {
		return "", fmt.Errorf("valkey disconnected")
	}
	return c.rdb.Get(ctx, KeyPrefixGeoIP+ip).Result()
}

func (c *Client) SetGeoIP(ctx context.Context, ip, location string, ttl time.Duration) error {
	if !c.IsConnected() {
		return nil
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour // default 30 days
	}
	return c.rdb.Set(ctx, KeyPrefixGeoIP+ip, location, ttl).Err()
}

func (c *Client) SetGeoIPBatch(ctx context.Context, entries map[string]string, ttl time.Duration) error {
	if !c.IsConnected() || len(entries) == 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	pipe := c.rdb.Pipeline()
	for ip, loc := range entries {
		pipe.Set(ctx, KeyPrefixGeoIP+ip, loc, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ----------------------------------------------------------------------
// SaaS Configuration Cache Operations (L2 Fast Configuration)
// ----------------------------------------------------------------------

func (c *Client) SetCachedSaaSConfig(ctx context.Context, raw map[string]string) error {
	if !c.IsConnected() || len(raw) == 0 {
		return nil
	}
	pipe := c.rdb.Pipeline()
	// Store as hash in config:saas
	pipe.HSet(ctx, KeyPrefixSaaSConfig, raw)
	// Also store models_config_json directly for instant JSON inspection
	if modelsJSON, ok := raw["models_config_json"]; ok && strings.TrimSpace(modelsJSON) != "" {
		pipe.Set(ctx, KeyModelsConfigJSON, modelsJSON, 0)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *Client) GetCachedSaaSConfig(ctx context.Context) (map[string]string, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("valkey disconnected")
	}
	return c.rdb.HGetAll(ctx, KeyPrefixSaaSConfig).Result()
}

// ----------------------------------------------------------------------
// Pub/Sub Notifications (Instant Multi-Instance Synchronization)
// ----------------------------------------------------------------------

func (c *Client) PublishConfigReload(ctx context.Context) error {
	if !c.IsConnected() {
		return nil
	}
	return c.rdb.Publish(ctx, ChannelConfigReload, "reload").Err()
}

func (c *Client) SubscribeConfigReload(ctx context.Context, reloadFn func()) {
	if !c.IsConnected() {
		return
	}

	pubsub := c.rdb.Subscribe(ctx, ChannelConfigReload)
	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()
		for msg := range ch {
			if msg != nil {
				log.Printf("[VALKEY PUB/SUB] Received %s signal. Reloading SaaS configurations.", ChannelConfigReload)
				reloadFn()
			}
		}
	}()
}

func (c *Client) PublishVocabInvalidate(ctx context.Context, userID int64) error {
	if !c.IsConnected() {
		return nil
	}
	return c.rdb.Publish(ctx, ChannelVocabInvalidate, strconv.FormatInt(userID, 10)).Err()
}

func (c *Client) SubscribeVocabInvalidate(ctx context.Context, invalidateFn func(userID int64)) {
	if !c.IsConnected() {
		return
	}

	pubsub := c.rdb.Subscribe(ctx, ChannelVocabInvalidate)
	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()
		for msg := range ch {
			if msg != nil && msg.Payload != "" {
				if uID, err := strconv.ParseInt(msg.Payload, 10, 64); err == nil {
					log.Printf("[VALKEY PUB/SUB] Received %s signal for User ID %d. Invalidating L1 cache.", ChannelVocabInvalidate, uID)
					invalidateFn(uID)
				}
			}
		}
	}()
}

// ----------------------------------------------------------------------
// Cached Sessions (L2 Fast Auth)
// ----------------------------------------------------------------------

type CachedSessionUser struct {
	ID                     int64    `json:"id"`
	Email                  string   `json:"email"`
	Name                   string   `json:"name"`
	PlanTier               string   `json:"plan_tier"`
	PlanActive             int      `json:"plan_active"`
	PlanActivatedAt        string   `json:"plan_activated_at"`
	RazorpaySubscriptionID string   `json:"razorpay_subscription_id"`
	RateLimitRPM           *int64   `json:"rate_limit_rpm,omitempty"`
	IsAdmin                int      `json:"is_admin"`
	IsTempSession          int      `json:"is_temp"`
	MonthlyUsageSeconds    float64  `json:"monthly_usage_seconds"`
	IsLocked               int      `json:"is_locked"`
	Languages              []string `json:"languages"`
	ExpiresAt              string   `json:"expires_at"`
	BillingPeriodEnd       *int64   `json:"billing_period_end,omitempty"`
}

func (c *Client) GetCachedSession(ctx context.Context, token string) (*CachedSessionUser, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("valkey disconnected")
	}
	data, err := c.rdb.Get(ctx, KeyPrefixSession+token).Bytes()
	if err != nil {
		return nil, err
	}
	var u CachedSessionUser
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) SetCachedSession(ctx context.Context, token string, session *CachedSessionUser, ttl time.Duration) error {
	if !c.IsConnected() || session == nil {
		return nil
	}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	pipe := c.rdb.Pipeline()
	pipe.Set(ctx, KeyPrefixSession+token, data, ttl)
	if session.ID > 0 {
		userSetKey := fmt.Sprintf("%s%d", KeyPrefixUserSessions, session.ID)
		pipe.SAdd(ctx, userSetKey, token)
		pipe.Expire(ctx, userSetKey, ttl+24*time.Hour)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (c *Client) InvalidateSession(ctx context.Context, token string) error {
	if !c.IsConnected() {
		return nil
	}
	// Try to get session to clean user_sessions set
	data, err := c.rdb.Get(ctx, KeyPrefixSession+token).Bytes()
	if err == nil {
		var u CachedSessionUser
		if json.Unmarshal(data, &u) == nil && u.ID > 0 {
			userSetKey := fmt.Sprintf("%s%d", KeyPrefixUserSessions, u.ID)
			_ = c.rdb.SRem(ctx, userSetKey, token).Err()
		}
	}
	return c.rdb.Del(ctx, KeyPrefixSession+token).Err()
}

func (c *Client) InvalidateUserSessions(ctx context.Context, userID int64) error {
	if !c.IsConnected() || userID <= 0 {
		return nil
	}
	userSetKey := fmt.Sprintf("%s%d", KeyPrefixUserSessions, userID)
	tokens, err := c.rdb.SMembers(ctx, userSetKey).Result()
	if err != nil {
		return err
	}
	pipe := c.rdb.Pipeline()
	for _, tok := range tokens {
		pipe.Del(ctx, KeyPrefixSession+tok)
	}
	pipe.Del(ctx, userSetKey)
	_, err = pipe.Exec(ctx)
	return err
}

// ----------------------------------------------------------------------
// Cached Vocabulary (L2 Fast Lookup & UI Visibility)
// ----------------------------------------------------------------------

type CachedVocabTerm struct {
	Canonical  string `json:"canonical_term"`
	Normalized string `json:"normalized_term"`
}

func (c *Client) GetCachedVocabulary(ctx context.Context, userID int64) ([]CachedVocabTerm, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("valkey disconnected")
	}
	data, err := c.rdb.Get(ctx, fmt.Sprintf("%s%d", KeyPrefixVocab, userID)).Bytes()
	if err != nil {
		return nil, err
	}
	var terms []CachedVocabTerm
	if err := json.Unmarshal(data, &terms); err != nil {
		return nil, err
	}
	return terms, nil
}

func (c *Client) SetCachedVocabulary(ctx context.Context, userID int64, terms []CachedVocabTerm, ttl time.Duration) error {
	if !c.IsConnected() {
		return nil
	}
	data, err := json.Marshal(terms)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return c.rdb.Set(ctx, fmt.Sprintf("%s%d", KeyPrefixVocab, userID), data, ttl).Err()
}

func (c *Client) InvalidateCachedVocabulary(ctx context.Context, userID int64) error {
	if !c.IsConnected() {
		return nil
	}
	return c.rdb.Del(ctx, fmt.Sprintf("%s%d", KeyPrefixVocab, userID)).Err()
}
