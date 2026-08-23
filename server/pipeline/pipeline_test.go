package pipeline

import (
	"testing"
)

func TestPipelineContext(t *testing.T) {
	pctx := NewPipelineContext(42)
	if pctx.UserID != 42 {
		t.Errorf("Expected UserID 42; got %d", pctx.UserID)
	}

	enhancer := NewAIEnhancementTransform("custom prompt", "deepseek-v4-flash", "test-key", "https://api.deepseek.com")
	if enhancer.Name() != "AI Enhancement" {
		t.Errorf("Expected name 'AI Enhancement'; got %s", enhancer.Name())
	}
}
