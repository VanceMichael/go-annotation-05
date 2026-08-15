package dispatch

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"nanhaiport/internal/store"
)

// slowTasks 构造一批模拟渔政网关调用的任务，每个任务最多阻塞 latency，
// 但必须在 context 取消后立即返回。
func slowTasks(n int, latency time.Duration, started *int32) []Task {
	tasks := make([]Task, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("T-%03d", i+1)
		tasks = append(tasks, Task{
			ID:      id,
			Subject: id,
			Kind:    "permit-approve",
			Run: func(ctx context.Context) error {
				atomic.AddInt32(started, 1)
				return GatewayCall(ctx, latency)
			},
		})
	}
	return tasks
}

func TestRunCompletesAllTasks(t *testing.T) {
	var started int32
	s := New(4, store.NewJournal())
	rep, err := s.Run(context.Background(), slowTasks(8, time.Millisecond, &started))
	if err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if rep.Total != 8 || rep.Done != 8 || rep.Failed != 0 || rep.Skipped != 0 {
		t.Fatalf("报告 = %+v, 期望全部成功", rep)
	}
	if rep.Aborted {
		t.Fatalf("未取消的调度不应标记为中止")
	}
}

// TestRunAbortsOnDeadline 断言调用方设置的超时会传播给任务，
// 整批调度必须在超时后立刻中止并返回超时错误。
func TestRunAbortsOnDeadline(t *testing.T) {
	var started int32
	const timeout = 100 * time.Millisecond
	const gatewayLatency = 5 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	s := New(3, store.NewJournal())
	begin := time.Now()
	rep, err := s.Run(ctx, slowTasks(12, gatewayLatency, &started))
	elapsed := time.Since(begin)

	if err == nil {
		t.Fatalf("超时后 Run 应返回错误, 实际返回 nil, 报告 = %+v", rep)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false, 错误为 %v", err)
	}
	if !rep.Aborted {
		t.Fatalf("报告应标记为中止, 实际 = %+v", rep)
	}
	if elapsed > time.Second {
		t.Fatalf("超时后耗时 = %v, 期望远小于 1s（网关单次耗时 %v）", elapsed, gatewayLatency)
	}
}

// TestRunAbortsOnCancel 断言调用方主动取消会传播给任务。
func TestRunAbortsOnCancel(t *testing.T) {
	var started int32
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	s := New(2, store.NewJournal())
	begin := time.Now()
	rep, err := s.Run(ctx, slowTasks(10, 5*time.Second, &started))
	elapsed := time.Since(begin)

	if err == nil {
		t.Fatalf("取消后 Run 应返回错误, 实际返回 nil, 报告 = %+v", rep)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false, 错误为 %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("取消后耗时 = %v, 期望远小于 1s", elapsed)
	}
}

// TestRunLeavesRemainingTasksUnstarted 断言超时后剩余任务不再下发。
func TestRunLeavesRemainingTasksUnstarted(t *testing.T) {
	var started int32
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	s := New(2, store.NewJournal())
	if _, err := s.Run(ctx, slowTasks(40, 5*time.Second, &started)); err == nil {
		t.Fatalf("超时后 Run 应返回错误")
	}
	if got := atomic.LoadInt32(&started); got > 10 {
		t.Fatalf("超时后已启动任务数 = %d, 期望远小于 40", got)
	}
}

func TestRunRejectsAlreadyCancelledContext(t *testing.T) {
	var started int32
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := New(2, store.NewJournal())
	rep, err := s.Run(ctx, slowTasks(5, time.Millisecond, &started))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("启动前已取消应返回 context.Canceled, 实际 %v", err)
	}
	if !rep.Aborted || rep.Skipped != 5 {
		t.Fatalf("报告 = %+v, 期望 aborted=true skipped=5", rep)
	}
	if got := atomic.LoadInt32(&started); got != 0 {
		t.Fatalf("启动前已取消时不应执行任务, 实际执行 %d 个", got)
	}
}

func TestRunRecordsFailures(t *testing.T) {
	boom := errors.New("网关返回 503")
	s := New(2, store.NewJournal())
	tasks := []Task{
		{ID: "T-1", Subject: "P-0001", Kind: "permit-approve", Run: func(ctx context.Context) error { return nil }},
		{ID: "T-2", Subject: "P-0002", Kind: "permit-approve", Run: func(ctx context.Context) error { return boom }},
		{ID: "T-3", Subject: "P-0003", Kind: "permit-approve", Run: func(ctx context.Context) error { return nil }},
	}
	rep, err := s.Run(context.Background(), tasks)
	if err != nil {
		t.Fatalf("普通任务失败不应导致整批中止: %v", err)
	}
	if rep.Done != 2 || rep.Failed != 1 || rep.Aborted {
		t.Fatalf("报告 = %+v, 期望 done=2 failed=1 aborted=false", rep)
	}
	if len(rep.Results) != 3 || rep.Results[0].TaskID != "T-1" {
		t.Fatalf("结果排序异常: %+v", rep.Results)
	}
}

func TestRunWritesJournal(t *testing.T) {
	j := store.NewJournal()
	s := New(2, j)
	var started int32
	if _, err := s.Run(context.Background(), slowTasks(6, time.Millisecond, &started)); err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if got := j.Len(); got != 6 {
		t.Fatalf("流水条数 = %d, 期望 6", got)
	}
}

func TestRunEmptyBatch(t *testing.T) {
	s := New(3, store.NewJournal())
	rep, err := s.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("空批次不应返回错误: %v", err)
	}
	if rep.Total != 0 || rep.Aborted {
		t.Fatalf("报告 = %+v", rep)
	}
}

func TestNewClampsWorkers(t *testing.T) {
	if got := New(0, nil).Workers(); got != 1 {
		t.Fatalf("workers = %d, 期望 1", got)
	}
	if got := New(-5, nil).Workers(); got != 1 {
		t.Fatalf("workers = %d, 期望 1", got)
	}
}

func TestGatewayCallRespectsContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	begin := time.Now()
	err := GatewayCall(ctx, 5*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GatewayCall 应返回 DeadlineExceeded, 实际 %v", err)
	}
	if time.Since(begin) > time.Second {
		t.Fatalf("GatewayCall 未及时返回")
	}
}
