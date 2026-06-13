package client

import (
	"encoding/json"
	"testing"
)

func TestUsage_DeepSeekCacheFields(t *testing.T) {
	// Real DeepSeek API response snippet (non-streaming completion).
	payload := `{
		"id": "chatcmpl-xxx",
		"object": "chat.completion",
		"model": "deepseek-v4-flash",
		"usage": {
			"prompt_tokens": 6,
			"completion_tokens": 5,
			"total_tokens": 11,
			"prompt_tokens_details": {
				"cached_tokens": 0
			},
			"completion_tokens_details": {
				"reasoning_tokens": 5
			},
			"prompt_cache_hit_tokens": 0,
			"prompt_cache_miss_tokens": 6
		}
	}`

	var resp struct {
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	u := resp.Usage
	if u.PromptTokens != 6 {
		t.Errorf("PromptTokens = %d, want 6", u.PromptTokens)
	}
	if u.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", u.CompletionTokens)
	}
	if u.PromptCacheHitTokens != 0 {
		t.Errorf("PromptCacheHitTokens = %d, want 0", u.PromptCacheHitTokens)
	}
	if u.PromptCacheMissTokens != 6 {
		t.Errorf("PromptCacheMissTokens = %d, want 6", u.PromptCacheMissTokens)
	}
}

func TestUsage_CacheHitScenario(t *testing.T) {
	// Second identical request: 6 tokens should all hit cache.
	payload := `{
		"usage": {
			"prompt_tokens": 6,
			"completion_tokens": 5,
			"total_tokens": 11,
			"prompt_cache_hit_tokens": 6,
			"prompt_cache_miss_tokens": 0
		}
	}`

	var resp struct {
		Usage Usage `json:"usage"`
	}
	json.Unmarshal([]byte(payload), &resp)
	u := resp.Usage

	// Cache hit rate = 6/6 = 100%
	if u.PromptCacheHitTokens != 6 {
		t.Errorf("expected 6 cache hits, got %d", u.PromptCacheHitTokens)
	}
	if u.PromptCacheMissTokens != 0 {
		t.Errorf("expected 0 cache misses, got %d", u.PromptCacheMissTokens)
	}
}

func TestUsage_LMStudioNoCacheFields(t *testing.T) {
	// LM Studio returns only basic fields, no cache metadata.
	payload := `{
		"usage": {
			"prompt_tokens": 12,
			"completion_tokens": 10,
			"total_tokens": 22,
			"completion_tokens_details": {
				"reasoning_tokens": 10
			}
		}
	}`

	var resp struct {
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		t.Fatalf("parse error: %v", err)
	}
	u := resp.Usage

	// Basic fields must parse.
	if u.PromptTokens != 12 {
		t.Errorf("PromptTokens = %d", u.PromptTokens)
	}
	// Cache fields should be zero (not present in payload).
	if u.PromptCacheHitTokens != 0 {
		t.Errorf("PromptCacheHitTokens should be 0 when absent, got %d", u.PromptCacheHitTokens)
	}
	if u.PromptCacheMissTokens != 0 {
		t.Errorf("PromptCacheMissTokens should be 0 when absent, got %d", u.PromptCacheMissTokens)
	}
}

func TestUsage_EmptyCacheFields(t *testing.T) {
	// Backward compat: plain OpenAI-compatible without cache fields.
	payload := `{
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 3,
			"total_tokens": 13
		}
	}`

	var resp struct {
		Usage Usage `json:"usage"`
	}
	json.Unmarshal([]byte(payload), &resp)
	u := resp.Usage

	if u.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d", u.PromptTokens)
	}
	if u.PromptCacheHitTokens != 0 {
		t.Errorf("cache hit should be 0: got %d", u.PromptCacheHitTokens)
	}
}

func TestUsage_CacheStatsString(t *testing.T) {
	u := Usage{
		PromptTokens:          100,
		PromptCacheHitTokens:  80,
		PromptCacheMissTokens: 20,
	}

	hitRate := float64(u.PromptCacheHitTokens) / float64(u.PromptTokens) * 100
	if hitRate != 80.0 {
		t.Errorf("hit rate = %.1f%%, want 80.0%%", hitRate)
	}
}
