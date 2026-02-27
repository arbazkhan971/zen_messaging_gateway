package utils

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	redisClient *redis.Client
	redisMu     sync.Mutex
)

// connectRedis establishes a connection to Redis.
func connectRedis() error {
	redisURL := "redis://:YOUR_STRONG_PASSWORD@34.100.179.109:6379/0"
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	redisMu.Lock()
	redisClient = client
	redisMu.Unlock()

	return nil
}

// ManageRedisConnection handles Redis connection and reconnection.
func ManageRedisConnection() {
	for {
		if err := connectRedis(); err != nil {
			log.Printf("Failed to connect to Redis: %v. Retrying in 5 seconds...", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("✅ Successfully connected to Redis.")

		// Monitor for disconnection (go-redis handles internally, but we can add a health check)
		// For simplicity, we'll loop on failure only from init
		// To detect disconnections, we can run a periodic ping
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := GetRedisClient().Ping(ctx).Err()
			cancel() // Cancel after the ping operation

			if err != nil {
				log.Printf("Redis connection lost: %v. Reconnecting...", err)
				break
			}
			time.Sleep(10 * time.Second) // Check every 10s
		}
	}
}

// GetRedisClient returns the current Redis client, safely.
func GetRedisClient() *redis.Client {
	redisMu.Lock()
	defer redisMu.Unlock()
	return redisClient
}

// TryDedupeMessage attempts to mark a message as processed using Redis SetNX.
// Returns true if the message is unique (first time), false if it's a duplicate.
// The key expires after the specified TTL to prevent memory leaks.
func TryDedupeMessage(ctx context.Context, messageID string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("msg_dedup:%s", messageID)
	client := GetRedisClient()
	if client == nil {
		return false, fmt.Errorf("redis client is nil")
	}

	// Create a timeout context for Redis operation (max 3 seconds)
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// SetNX returns true if key was set (first time), false if key already exists
	set, err := client.SetNX(timeoutCtx, key, "processed", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("redis SetNX failed: %w", err)
	}
	return set, nil
}

// BatchTryDedupeMessages attempts to mark multiple messages as processed using Redis pipeline.
// Returns a map of messageID -> isUnique (true if first time, false if duplicate).
// The keys expire after the specified TTL to prevent memory leaks.
func BatchTryDedupeMessages(ctx context.Context, messageIDs []string, ttl time.Duration) (map[string]bool, error) {
	if len(messageIDs) == 0 {
		return make(map[string]bool), nil
	}

	client := GetRedisClient()
	if client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	// Create a timeout context for Redis operation (max 5 seconds for batch)
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Use pipeline for batch operations
	pipe := client.Pipeline()

	// Prepare all SetNX commands
	cmds := make(map[string]*redis.BoolCmd)
	for _, messageID := range messageIDs {
		key := fmt.Sprintf("msg_dedup:%s", messageID)
		cmds[messageID] = pipe.SetNX(timeoutCtx, key, "processed", ttl)
	}

	// Execute pipeline
	_, err := pipe.Exec(timeoutCtx)
	if err != nil {
		return nil, fmt.Errorf("redis pipeline failed: %w", err)
	}

	// Collect results
	results := make(map[string]bool)
	for messageID, cmd := range cmds {
		if cmd.Err() != nil {
			log.Printf("[BatchDedup] Redis SetNX failed for message %s: %v", messageID, cmd.Err())
			// Treat Redis errors as duplicates to be safe
			results[messageID] = false
		} else {
			results[messageID] = cmd.Val()
		}
	}

	return results, nil
}

// BatchIsMessageProcessed checks if multiple messages have already been processed.
// Returns a map of messageID -> isProcessed (true if already processed, false otherwise).
func BatchIsMessageProcessed(ctx context.Context, messageIDs []string) (map[string]bool, error) {
	if len(messageIDs) == 0 {
		return make(map[string]bool), nil
	}

	client := GetRedisClient()
	if client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	// Create a timeout context for Redis operation (max 5 seconds for batch)
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Use pipeline for batch operations
	pipe := client.Pipeline()

	// Prepare all Exists commands
	cmds := make(map[string]*redis.IntCmd)
	for _, messageID := range messageIDs {
		key := fmt.Sprintf("msg_dedup:%s", messageID)
		cmds[messageID] = pipe.Exists(timeoutCtx, key)
	}

	// Execute pipeline
	_, err := pipe.Exec(timeoutCtx)
	if err != nil {
		return nil, fmt.Errorf("redis pipeline failed: %w", err)
	}

	// Collect results
	results := make(map[string]bool)
	for messageID, cmd := range cmds {
		if cmd.Err() != nil {
			log.Printf("[BatchCheck] Redis Exists failed for message %s: %v", messageID, cmd.Err())
			// Treat Redis errors as already processed to be safe
			results[messageID] = true
		} else {
			results[messageID] = cmd.Val() > 0
		}
	}

	return results, nil
}

// UnmarkMessage removes a message from the deduplication set.
// This is used when message processing fails and we want to allow retry.
func UnmarkMessage(ctx context.Context, messageID string) error {
	key := fmt.Sprintf("msg_dedup:%s", messageID)
	client := GetRedisClient()
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}

	err := client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("redis Del failed: %w", err)
	}
	return nil
}

// IsMessageProcessed checks if a message has already been processed.
// Returns true if the message exists in Redis (already processed), false otherwise.
func IsMessageProcessed(ctx context.Context, messageID string) (bool, error) {
	key := fmt.Sprintf("msg_dedup:%s", messageID)
	client := GetRedisClient()
	if client == nil {
		return false, fmt.Errorf("redis client is nil")
	}

	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("redis Exists failed: %w", err)
	}
	return exists > 0, nil
}

// SetCategoryChangeFlag sets a Redis flag indicating that a campaign's template category has changed.
// This flag is checked by the consumer to determine if the campaign should be paused.
// Key format: category_change:{campaignID}:{templateName}
// Value: "changed:{oldCategory}:{newCategory}:{timestamp}"
// TTL: 24 hours to allow time for processing
func SetCategoryChangeFlag(ctx context.Context, campaignID, templateName, oldCategory, newCategory string) error {
	key := fmt.Sprintf("category_change:%s:%s", campaignID, templateName)
	value := fmt.Sprintf("changed:%s:%s:%d", oldCategory, newCategory, time.Now().Unix())
	client := GetRedisClient()
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	err := client.Set(timeoutCtx, key, value, 24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("redis Set failed for category change flag: %w", err)
	}

	log.Printf("[REDIS] Set category change flag for campaign %s, template %s: %s -> %s", campaignID, templateName, oldCategory, newCategory)
	return nil
}

// GetCategoryChangeFlag checks if a category change flag exists for the given campaign and template.
// Returns the change details if the flag exists, or empty strings if no change is flagged.
func GetCategoryChangeFlag(ctx context.Context, campaignID, templateName string) (oldCategory, newCategory string, exists bool, err error) {
	key := fmt.Sprintf("category_change:%s:%s", campaignID, templateName)
	client := GetRedisClient()
	if client == nil {
		return "", "", false, fmt.Errorf("redis client is nil")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	value, err := client.Get(timeoutCtx, key).Result()
	if err == redis.Nil {
		return "", "", false, nil // No flag exists
	}
	if err != nil {
		return "", "", false, fmt.Errorf("redis Get failed for category change flag: %w", err)
	}

	// Parse the value: "changed:{oldCategory}:{newCategory}:{timestamp}"
	var timestamp int64
	_, err = fmt.Sscanf(value, "changed:%s:%s:%d", &oldCategory, &newCategory, &timestamp)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to parse category change flag value: %w", err)
	}

	return oldCategory, newCategory, true, nil
}

// ClearCategoryChangeFlag removes the category change flag after the campaign has been paused.
func ClearCategoryChangeFlag(ctx context.Context, campaignID, templateName string) error {
	key := fmt.Sprintf("category_change:%s:%s", campaignID, templateName)
	client := GetRedisClient()
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	err := client.Del(timeoutCtx, key).Err()
	if err != nil {
		return fmt.Errorf("redis Del failed for category change flag: %w", err)
	}

	log.Printf("[REDIS] Cleared category change flag for campaign %s, template %s", campaignID, templateName)
	return nil
}
