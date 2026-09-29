// Package msgedit owns the bounded, asynchronous Bot message edit queue.
package msgedit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type Result string

const (
	Accepted Result = "accepted"
	Merged   Result = "merged"
	Expired  Result = "expired"
	Closed   Result = "closed"
	Invalid  Result = "invalid"
)

type Phase uint8

const (
	Queued Phase = iota
	Progress
	Cancelling
	Final
)

type Key struct {
	ChatID    int64
	MessageID int
}

var sequence atomic.Uint64

// Lifecycle is retained by the task, rather than as a permanent queue tombstone.
// Its state is protected by the owning pool's mutex.
type Lifecycle struct {
	TaskID                              string
	order                               uint64
	owner                               *Pool
	started, cancelled, sealed, evicted bool
}

func NewLifecycle(taskID string) *Lifecycle {
	return &Lifecycle{TaskID: taskID, order: sequence.Add(1)}
}

type Sender func(context.Context, int64, *tg.MessagesEditMessageRequest) error
type snapshot struct {
	request *tg.MessagesEditMessageRequest
	life    *Lifecycle
	phase   Phase
	order   uint64
}
type Pool struct {
	mu       sync.Mutex
	cond     *sync.Cond
	ctx      context.Context
	cancel   context.CancelFunc
	send     Sender
	capacity int
	pending  map[Key]*snapshot
	active   map[Key]*snapshot
	// Retain live ownership across drained sends, not completed-message history.
	owners map[Key]*Lifecycle
	closed bool
	done   chan struct{}
}

func New(ctx context.Context, workers, capacity int, send Sender) *Pool {
	if workers < 1 || capacity < 1 || send == nil {
		panic("invalid message edit pool options")
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Pool{ctx: ctx, cancel: cancel, send: send, capacity: capacity, pending: make(map[Key]*snapshot), active: make(map[Key]*snapshot), owners: make(map[Key]*Lifecycle), done: make(chan struct{})}
	p.cond = sync.NewCond(&p.mu)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(p.worker)
	}
	go func() { wg.Wait(); close(p.done) }()
	context.AfterFunc(ctx, p.stop)
	return p
}

// Submit copies the request before admission. It never waits for an RPC, token,
// message lock, or free queue slot. Only unsent patches can be merged.
func (p *Pool) Submit(chatID int64, req *tg.MessagesEditMessageRequest, life *Lifecycle, phase Phase) (Result, error) {
	if req == nil || req.ID <= 0 || chatID == 0 {
		return Invalid, fmt.Errorf("invalid edit target")
	}
	owned := cloneRequest(req)
	key := Key{chatID, req.ID}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.ctx.Err() != nil {
		return Closed, nil
	}
	if life != nil {
		if life.owner != nil && life.owner != p {
			return Invalid, fmt.Errorf("notification belongs to another pool")
		}
		life.owner = p
		if life.evicted || life.sealed || (phase == Queued && (life.started || life.cancelled)) || (phase == Progress && life.cancelled) {
			return Expired, nil
		}
		switch phase {
		case Progress:
			life.started = true
		case Cancelling:
			life.cancelled = true
		case Final:
			life.sealed = true
		}
	}
	order := sequence.Add(1)
	if life != nil {
		order = life.order
	}
	next := &snapshot{request: owned, life: life, phase: phase, order: order}
	if owner := p.owners[key]; owner != nil && owner != life && owner.order > order {
		p.discard(key, next, "superseded")
		return Expired, nil
	}
	if active := p.active[key]; active != nil && active.life != life {
		if active.order > order {
			p.discard(key, next, "superseded")
			return Expired, nil
		}
		if active.life != nil {
			active.life.evicted = true
		}
	}
	if previous := p.pending[key]; previous != nil {
		// A newly created task may explicitly reuse a message, but an older task
		// cannot overwrite the newer task's pending notification.
		if previous.life != life && previous.order > order {
			p.discard(key, next, "superseded")
			return Expired, nil
		}
		if previous.life == life {
			next.request = merge(previous.request, owned)
			next.order = previous.order
		} else {
			p.discard(key, previous, "message reused")
		}
		p.pending[key] = next
		p.claim(key, life)
		p.cond.Broadcast()
		return Merged, nil
	}
	if len(p.pending) == p.capacity {
		var victimKey Key
		var victim *snapshot
		// Only older notifications yield to this arrival. A high-frequency old task
		// cannot turn itself into a new task by resubmitting after its slot was sent.
		for k, v := range p.pending {
			if v.order < order && (victim == nil || olderVictim(v, victim)) {
				victimKey, victim = k, v
			}
		}
		if victim == nil {
			p.discard(key, next, "capacity")
			return Expired, nil
		}
		delete(p.pending, victimKey)
		p.discard(victimKey, victim, "capacity")
	}
	p.pending[key] = next
	p.claim(key, life)
	p.cond.Broadcast()
	return Accepted, nil
}

func (p *Pool) claim(key Key, life *Lifecycle) {
	if previous := p.owners[key]; previous != nil && previous != life {
		previous.evicted = true
		p.releaseOwners(previous)
	}
	if life == nil {
		delete(p.owners, key)
	} else {
		p.owners[key] = life
	}
}

// Closed lifecycles keep their own rejection flag. Once their accepted work
// drains, the pool no longer needs to retain their message keys.
func (p *Pool) releaseOwners(life *Lifecycle) {
	if life == nil || (!life.sealed && !life.evicted) {
		return
	}
	for key, owner := range p.owners {
		if owner != life {
			continue
		}
		if pending := p.pending[key]; pending != nil && pending.life == life {
			continue
		}
		if active := p.active[key]; active != nil && active.life == life {
			continue
		}
		delete(p.owners, key)
	}
}

func olderVictim(a, b *snapshot) bool {
	if (a.phase == Final) != (b.phase == Final) {
		return a.phase != Final
	}
	return a.order < b.order
}
func (p *Pool) discard(key Key, s *snapshot, reason string) {
	if s.life != nil {
		s.life.evicted = true
		p.releaseOwners(s.life)
	}
	taskID := ""
	if s.life != nil {
		taskID = s.life.TaskID
	}
	log.FromContext(p.ctx).Warn("Message edit evicted", "chat", key.ChatID, "message", key.MessageID, "task", taskID, "terminal", s.phase == Final, "reason", reason)
}
func (p *Pool) worker() {
	for {
		p.mu.Lock()
		var key Key
		var job *snapshot
		for !p.closed {
			for k, v := range p.pending {
				if p.active[k] == nil && (job == nil || v.order > job.order) {
					key, job = k, v
				}
			}
			if job != nil {
				break
			}
			p.cond.Wait()
		}
		if p.closed {
			p.mu.Unlock()
			return
		}
		delete(p.pending, key)
		p.active[key] = job
		p.mu.Unlock()
		err := p.send(p.ctx, key.ChatID, job.request)
		if err != nil && !tgerr.Is(err, "MESSAGE_NOT_MODIFIED") {
			taskID := ""
			if job.life != nil {
				taskID = job.life.TaskID
			}
			log.FromContext(p.ctx).Error("Message edit failed", "chat", key.ChatID, "message", key.MessageID, "task", taskID, "error", err)
		}
		p.mu.Lock()
		delete(p.active, key)
		p.releaseOwners(job.life)
		p.cond.Broadcast()
		p.mu.Unlock()
	}
}
func (p *Pool) stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	log.FromContext(p.ctx).Info("Stopping message edits", "pending", len(p.pending), "active", len(p.active))
	clear(p.pending)
	clear(p.owners)
	p.cancel()
	p.cond.Broadcast()
}

// Close cancels SDK waits and bounds shutdown even if a sender ignores context.
func (p *Pool) Close(timeout time.Duration) bool {
	p.stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return true
	case <-timer.C:
		return false
	}
}

// Cancel invalidates unsent progress without taking ownership of the final text.
// The return value says whether execution has begun.
func (p *Pool) Cancel(life *Lifecycle) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	life.cancelled = true
	for key, s := range p.pending {
		if s.life == life && s.phase != Final {
			delete(p.pending, key)
		}
	}
	p.releaseOwners(life)
	return life.started
}
func (p *Pool) Started(life *Lifecycle) {
	p.mu.Lock()
	defer p.mu.Unlock()
	life.started = true
}

// Seal closes the lifecycle when task execution ends without another edit (for
// example an early rendering error). It does not remove an accepted snapshot.
func (p *Pool) Seal(life *Lifecycle) {
	p.mu.Lock()
	defer p.mu.Unlock()
	life.sealed = true
	p.releaseOwners(life)
}
