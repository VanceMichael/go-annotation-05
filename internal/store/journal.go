// Package store 负责调度流水与统计结果的落盘与回放。
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 表示一条调度流水。
type Entry struct {
	Seq      int       `json:"seq"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	Subject  string    `json:"subject"`
	Operator string    `json:"operator"`
	Detail   string    `json:"detail"`
	Success  bool      `json:"success"`
}

// Journal 是内存中的调度流水账，可整体落盘为 JSON Lines 文件。
type Journal struct {
	mu      sync.Mutex
	entries []Entry
	seq     int
}

// NewJournal 构造空流水账。
func NewJournal() *Journal {
	return &Journal{}
}

// Append 追加一条流水，返回写入后的条目。
func (j *Journal) Append(e Entry) Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	e.Seq = j.seq
	if e.At.IsZero() {
		e.At = time.Unix(0, 0).UTC()
	}
	j.entries = append(j.entries, e)
	return e
}

// Len 返回流水条数。
func (j *Journal) Len() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.entries)
}

// Entries 返回流水副本，按序号排序。
func (j *Journal) Entries() []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Entry, len(j.entries))
	copy(out, j.entries)
	sort.Slice(out, func(a, b int) bool { return out[a].Seq < out[b].Seq })
	return out
}

// Failures 返回失败的流水条目。
func (j *Journal) Failures() []Entry {
	all := j.Entries()
	out := make([]Entry, 0, len(all))
	for _, e := range all {
		if !e.Success {
			out = append(out, e)
		}
	}
	return out
}

// Flush 将全部流水写入 path 指向的 JSON Lines 文件。
// 目标目录会按需创建；已存在的文件被整体覆盖。
// 函数返回时文件内容必须已经完整落盘，随后可直接由 Replay 读回。
func (j *Journal) Flush(path string) (err error) {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("store: 流水落盘路径为空")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return fmt.Errorf("store: 创建目录 %s 失败: %w", dir, mkErr)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("store: 创建流水文件 %s 失败: %w", path, err)
	}
	w := bufio.NewWriter(f)
	defer func() {
		if flushErr := w.Flush(); err == nil && flushErr != nil {
			err = fmt.Errorf("store: 刷新流水文件 %s 失败: %w", path, flushErr)
		}
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("store: 关闭流水文件 %s 失败: %w", path, closeErr)
		}
	}()

	enc := json.NewEncoder(w)
	for _, e := range j.Entries() {
		if encErr := enc.Encode(e); encErr != nil {
			return fmt.Errorf("store: 写入流水第 %d 条失败: %w", e.Seq, encErr)
		}
	}
	return nil
}

// Replay 从 JSON Lines 文件读回流水。
func Replay(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("store: 打开流水文件 %s 失败: %w", path, err)
	}
	defer f.Close()

	var out []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var e Entry
		if uerr := json.Unmarshal([]byte(text), &e); uerr != nil {
			return nil, fmt.Errorf("store: 解析流水文件 %s 第 %d 行失败: %w", path, line, uerr)
		}
		out = append(out, e)
	}
	if serr := scanner.Err(); serr != nil {
		return nil, fmt.Errorf("store: 读取流水文件 %s 失败: %w", path, serr)
	}
	return out, nil
}

// SaveJSON 将任意结果对象写为缩进 JSON 文件。
func SaveJSON(path string, payload any) (err error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return fmt.Errorf("store: 创建目录 %s 失败: %w", dir, mkErr)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("store: 创建文件 %s 失败: %w", path, err)
	}
	w := bufio.NewWriter(f)
	defer func() {
		if flushErr := w.Flush(); err == nil && flushErr != nil {
			err = fmt.Errorf("store: 刷新文件 %s 失败: %w", path, flushErr)
		}
		if closeErr := f.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("store: 关闭文件 %s 失败: %w", path, closeErr)
		}
	}()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(payload); encErr != nil {
		return fmt.Errorf("store: 序列化 %s 失败: %w", path, encErr)
	}
	return nil
}
