// Package dispatch 实现开渔期批量审批与靠泊调度的并发执行器。
//
// 调度器按固定并发度消费任务队列。调用方通过 context 控制整批调度的
// 生命周期：一旦调用方取消或超时，正在执行的任务必须尽快感知并退出，
// 未开始的任务不再下发，整批调度以中止错误返回。
package dispatch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"nanhaiport/internal/store"
)

// Task 表示一个可调度的审批任务。
type Task struct {
	ID      string
	Subject string
	Kind    string
	Run     func(ctx context.Context) error
}

// Result 表示一个任务的执行结果。
type Result struct {
	TaskID  string        `json:"task_id"`
	Subject string        `json:"subject"`
	Kind    string        `json:"kind"`
	Err     error         `json:"-"`
	Message string        `json:"message"`
	Elapsed time.Duration `json:"elapsed_ns"`
}

// Report 汇总一批调度的执行结果。
type Report struct {
	Total    int           `json:"total"`
	Done     int           `json:"done"`
	Failed   int           `json:"failed"`
	Skipped  int           `json:"skipped"`
	Aborted  bool          `json:"aborted"`
	Elapsed  time.Duration `json:"elapsed_ns"`
	Results  []Result      `json:"results"`
	Workers  int           `json:"workers"`
	Deadline string        `json:"deadline,omitempty"`
}

// Scheduler 按固定并发度执行批量任务。
type Scheduler struct {
	workers int
	journal *store.Journal
}

// New 构造调度器。workers 小于 1 时按 1 处理。
func New(workers int, journal *store.Journal) *Scheduler {
	if workers < 1 {
		workers = 1
	}
	return &Scheduler{workers: workers, journal: journal}
}

// Workers 返回并发度。
func (s *Scheduler) Workers() int {
	return s.workers
}

// Run 并发执行 tasks。
//
// ctx 是整批调度的生命周期控制器：ctx 被取消或超时后，未开始的任务不再
// 下发，已开始的任务会收到派生自 ctx 的取消信号，Run 以中止错误返回，
// Report.Aborted 置为 true。
func (s *Scheduler) Run(ctx context.Context, tasks []Task) (Report, error) {
	rep := Report{Total: len(tasks), Workers: s.workers}
	if deadline, ok := ctx.Deadline(); ok {
		rep.Deadline = deadline.UTC().Format(time.RFC3339Nano)
	}
	if err := ctx.Err(); err != nil {
		rep.Aborted = true
		rep.Skipped = len(tasks)
		return rep, fmt.Errorf("dispatch: 批量调度启动前已被取消: %w", err)
	}
	if len(tasks) == 0 {
		return rep, nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	queue := make(chan Task)
	results := make(chan Result, len(tasks))
	begin := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < s.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range queue {
				results <- s.execute(runCtx, task)
			}
		}()
	}

	go func() {
		defer close(queue)
		for _, task := range tasks {
			select {
			case <-runCtx.Done():
				return
			case queue <- task:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var abortErr error
	for res := range results {
		rep.Results = append(rep.Results, res)
		switch {
		case res.Err == nil:
			rep.Done++
		default:
			rep.Failed++
			if isContextError(res.Err) && abortErr == nil {
				abortErr = res.Err
				cancel()
			}
		}
	}
	rep.Elapsed = time.Since(begin)
	sort.Slice(rep.Results, func(i, j int) bool { return rep.Results[i].TaskID < rep.Results[j].TaskID })
	rep.Skipped = rep.Total - len(rep.Results)

	if abortErr != nil {
		rep.Aborted = true
		return rep, fmt.Errorf("dispatch: 批量调度中止: %w", abortErr)
	}
	return rep, nil
}

// execute 执行单个任务并写入流水。
func (s *Scheduler) execute(ctx context.Context, task Task) Result {
	begin := time.Now()
	var err error
	if task.Run != nil {
		err = task.Run(ctx)
	}
	res := Result{TaskID: task.ID, Subject: task.Subject, Kind: task.Kind, Err: err, Elapsed: time.Since(begin)}
	if err != nil {
		res.Message = err.Error()
	} else {
		res.Message = "ok"
	}
	if s.journal != nil {
		s.journal.Append(store.Entry{
			At:      begin.UTC(),
			Kind:    task.Kind,
			Subject: task.Subject,
			Detail:  res.Message,
			Success: err == nil,
		})
	}
	return res
}

// isContextError 报告错误链中是否含有 context 取消或超时。
func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// GatewayCall 模拟一次渔政审批网关调用：等待 latency 或提前响应取消信号。
// 网关调用必须尊重传入的 context，取消后立即返回 ctx.Err()。
func GatewayCall(ctx context.Context, latency time.Duration) error {
	if latency <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(latency)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
