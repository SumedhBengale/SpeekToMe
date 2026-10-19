package pipeline

import (
	"context"
	"fmt"
)

// CommandBudget is installed only on Command Mode requests. Each provider attempt
// reserves its own allowance, so fallback cannot bypass monthly accounting.
type CommandBudget interface {
	Reserve(context.Context, string, string, string, string) (int64, error)
	Usage(input, output int64)
}
type commandBudgetKey struct{}

func WithCommandBudget(ctx context.Context, budget CommandBudget) context.Context {
	return context.WithValue(ctx, commandBudgetKey{}, budget)
}
func commandBudget(ctx context.Context) CommandBudget {
	b, _ := ctx.Value(commandBudgetKey{}).(CommandBudget)
	return b
}

type tokenUsage struct {
	Input  int64 `json:"prompt_tokens"`
	Output int64 `json:"completion_tokens"`
}

func reportUsage(ctx context.Context, usage *tokenUsage) {
	if b := commandBudget(ctx); b != nil && usage != nil {
		b.Usage(usage.Input, usage.Output)
	}
}

func commandOutputLimit(ctx context.Context) error {
	if budget, ok := commandBudget(ctx).(interface{ OutputLimitError() error }); ok {
		return budget.OutputLimitError()
	}
	return fmt.Errorf("Command Mode output limit reached. Try transforming a shorter selection.")
}
