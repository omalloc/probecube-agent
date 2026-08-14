package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/omalloc/probecube-agent/internal/engine"
)

type mockConfig struct {
	async    bool
	poolSize int32
}

func (c *mockConfig) GetAsync() bool     { return c.async }
func (c *mockConfig) GetPoolSize() int32 { return c.poolSize }

type mockProber struct {
	delay time.Duration
	err   error
}

func (m *mockProber) Probe(ctx context.Context, task *engine.Task) (*engine.Result, error) {
	select {
	case <-time.After(m.delay):
		if m.err != nil {
			return nil, m.err
		}
		return &engine.Result{TaskID: task.ID, Success: true, Status: "OK"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestScheduler_Concurrency(t *testing.T) {
	engine.Register("MOCK", &mockProber{delay: 100 * time.Millisecond})

	s := NewSchedulerServer(&mockConfig{async: true, poolSize: 2})
	s.Start(t.Context())

	ctx := context.Background()
	start := time.Now()

	// 提交 4 个任务，每个耗时 100ms，并发限制为 2
	// 总耗时应接近 200ms
	for i := 0; i < 4; i++ {
		s.Submit(ctx, &engine.Task{TaskSN: "test", Proto: "MOCK"})
		t.Logf("add engine.task %d", i)
	}

	done := make(chan struct{}, 4)
	go func() {
		for result := range s.Results() {
			t.Logf("process engine.result=%#+v", result)
			done <- struct{}{}
		}
	}()

	t.Logf("waiting for results")
	for i := 0; i < 4; i++ {
		<-done
	}
	duration := time.Since(start)

	if duration < 200*time.Millisecond || duration > 300*time.Millisecond {
		t.Errorf("Expected duration around 200ms, got %v", duration)
	}
}

func TestScheduler_Error(t *testing.T) {
	engine.Register("ERR", &mockProber{err: errors.New("failed")})

	s := NewSchedulerServer(&mockConfig{async: false, poolSize: 1})

	s.Submit(context.Background(), &engine.Task{TaskSN: "E1", Proto: "ERR"})

	result := <-s.Results()
	if result.Success {
		t.Error("Expected failure, got success")
	}
	if result.Status != "Probe Failed" {
		t.Errorf("Expected status 'Probe Failed', got %s", result.Status)
	}
}

func TestScheduler_TargetPassthrough(t *testing.T) {
	tests := []struct {
		name   string
		proto  engine.Protocol
		status string
	}{
		{"probe_error", "MOCKT", "Probe Failed"},
		{"unsupported", "NOPE", "Unsupported Protocol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine.Register("MOCKT", &mockProber{err: errors.New("failed")})

			s := NewSchedulerServer(&mockConfig{async: false, poolSize: 1})
			s.Submit(context.Background(), &engine.Task{
				ID: 7, TaskSN: "target-test", Proto: tt.proto, Target: "example.com",
			})

			result := <-s.Results()
			if result.Target != "example.com" {
				t.Fatalf("result.Target = %q, want example.com", result.Target)
			}
			if result.Status != tt.status {
				t.Fatalf("result.Status = %q, want %q", result.Status, tt.status)
			}
		})
	}
}

func TestScheduler_SuccessPassthroughTarget(t *testing.T) {
	engine.Register("MOCKS", &mockProber{})

	s := NewSchedulerServer(&mockConfig{async: false, poolSize: 1})
	s.Submit(context.Background(), &engine.Task{
		ID: 7, TaskSN: "target-ok", Proto: "MOCKS", Target: "example.org",
	})

	result := <-s.Results()
	if result.Target != "example.org" {
		t.Fatalf("result.Target = %q, want example.org", result.Target)
	}
	if !result.Success {
		t.Fatalf("result.Success = false, want true")
	}
}
