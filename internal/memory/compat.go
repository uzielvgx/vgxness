package memory

import "context"

// Save and Search remain thin source-compatibility adapters for integrations
// compiled against the previous Go surface. They hold no separate contract or
// implementation and immediately enter the native core.
// Deprecated: use Remember.
func (s MemoryService) Save(ctx context.Context, request SaveRequest) (MemoryResult, error) {
	return s.Remember(ctx, request)
}

// Deprecated: use Recall.
func (s MemoryService) Search(ctx context.Context, request SearchRequest) ([]MemoryResult, error) {
	return s.Recall(ctx, request)
}
