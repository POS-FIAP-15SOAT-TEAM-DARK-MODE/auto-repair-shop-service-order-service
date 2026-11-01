package saga

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-service-order-service/internal/pkg/messaging"
)

// MemoryRepository is an in-memory Repository used by unit tests and the in-memory deployment.
type MemoryRepository struct {
	mu    sync.Mutex
	items map[string]Instance
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: map[string]Instance{}}
}

func (r *MemoryRepository) Insert(_ context.Context, inst *Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[inst.ID] = *inst
	return nil
}

func (r *MemoryRepository) Update(_ context.Context, inst *Instance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[inst.ID] = *inst
	return nil
}

func (r *MemoryRepository) FindByID(_ context.Context, id string) (*Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.items[id]
	if !ok {
		return nil, nil
	}
	return copyInstance(inst), nil
}

func (r *MemoryRepository) FindRunningBySO(_ context.Context, soID string) (*Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inst := range r.items {
		if inst.ServiceOrderID == soID && inst.Status == StatusRunning {
			return copyInstance(inst), nil
		}
	}
	return nil, nil
}

func (r *MemoryRepository) FindExpired(_ context.Context, now time.Time, limit int) ([]Instance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Instance
	for _, inst := range r.items {
		if inst.Status == StatusRunning && inst.Deadline != nil && inst.Deadline.Before(now) {
			out = append(out, inst)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func copyInstance(inst Instance) *Instance {
	return &inst
}

// SentMessage is one outbox entry recorded by MemoryOutbox.
type SentMessage struct {
	Topic  string
	Target string
	Env    messaging.Envelope
}

// MemoryOutbox records the messages instead of storing them in the outbox table.
type MemoryOutbox struct {
	mu   sync.Mutex
	Sent []SentMessage
}

func NewMemoryOutbox() *MemoryOutbox {
	return &MemoryOutbox{}
}

func (o *MemoryOutbox) Enqueue(_ context.Context, topic, target string, env messaging.Envelope) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Sent = append(o.Sent, SentMessage{Topic: topic, Target: target, Env: env})
	return nil
}

// Types returns the message types recorded so far, in order.
func (o *MemoryOutbox) Types() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	types := make([]string, 0, len(o.Sent))
	for _, m := range o.Sent {
		types = append(types, m.Env.Type)
	}
	return types
}
