package server

import (
	"context"

	"github.com/go-kratos/kratos/v3/log"
	"github.com/omalloc/probecube-agent/internal/engine"
	_ "github.com/omalloc/probecube-agent/internal/engine/probers"
)

var _ engine.ProbeEngineScheduler = (*Scheduler)(nil)

const (
	UnsupportedProtocol = "Unsupported Protocol"
	ProbeFailed         = "Probe Failed"
)

type SchedulerConfig interface {
	GetAsync() bool
	GetPoolSize() int32
}

type Scheduler struct {
	async       bool
	concurrency int
	sem         chan struct{}
	tasks       chan *engine.Task
	results     chan *engine.Result
	stopCh      chan struct{}
}

func NewSchedulerServer(c SchedulerConfig) engine.ProbeEngineScheduler {
	return &Scheduler{
		async:       c.GetAsync(),
		concurrency: int(c.GetPoolSize()),
		sem:         make(chan struct{}, int(c.GetPoolSize())),
		tasks:       make(chan *engine.Task, 100),
		results:     make(chan *engine.Result, 1000),
		stopCh:      make(chan struct{}, 1),
	}
}

func (s *Scheduler) Submit(ctx context.Context, task *engine.Task) {
	// 异步任务
	if s.async {
		go func() {
			select {
			case s.tasks <- task:
			case <-ctx.Done():
				return
			}
		}()
		return
	}

	// 同步任务直接执行，不依赖 Start 启动的异步队列消费协程。
	s.dispatch(ctx, task)
}

func (s *Scheduler) Results() <-chan *engine.Result {
	return s.results
}

func (s *Scheduler) tick(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case task, ok := <-s.tasks:
			if !ok {
				return
			}

			go s.dispatch(ctx, task)
		}
	}
}

func (s *Scheduler) dispatch(ctx context.Context, task *engine.Task) {
	// 2. 获取并发许可，执行探测任务
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return
	}

	prober, ok := engine.MatchProber(task.Proto)
	if !ok {
		s.results <- &engine.Result{
			TaskID:  task.ID,
			RunID:   task.RunID,
			TaskSN:  task.TaskSN,
			Type:    task.Type,
			Proto:   task.Proto,
			Success: false,
			Status:  UnsupportedProtocol,
		}
		return
	}

	result, err := prober.Probe(ctx, task)
	if err != nil {
		s.results <- &engine.Result{
			TaskID:  task.ID,
			RunID:   task.RunID,
			TaskSN:  task.TaskSN,
			Type:    task.Type,
			Proto:   task.Proto,
			Success: false,
			Status:  ProbeFailed,
			Err:     err,
		}
		return
	}

	// Preserve dispatch identity even for third-party probers that only return probe metrics.
	result.TaskID = task.ID
	result.RunID = task.RunID
	result.TaskSN = task.TaskSN
	s.results <- result
}

// Start implements [transport.Server].
func (s *Scheduler) Start(ctx context.Context) error {

	log.Info("start engine scheduler")

	go s.tick(ctx)

	return nil
}

// Stop implements [transport.Server].
func (s *Scheduler) Stop(context.Context) error {

	s.stopCh <- struct{}{}

	close(s.results)

	return nil
}
