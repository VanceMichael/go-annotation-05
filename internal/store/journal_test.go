package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fill(j *Journal, n int) {
	base := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		j.Append(Entry{
			At:       base.Add(time.Duration(i) * time.Minute),
			Kind:     "permit-approve",
			Subject:  "P-0001",
			Operator: "渔政",
			Detail:   "闸坡渔港开渔首航审批",
			Success:  i%3 != 0,
		})
	}
}

// TestFlushPersistsAllEntries 断言 Flush 返回后文件已包含全部流水，可立即回放。
func TestFlushPersistsAllEntries(t *testing.T) {
	cases := []int{1, 5, 37}
	for _, n := range cases {
		j := NewJournal()
		fill(j, n)
		path := filepath.Join(t.TempDir(), "journal.jsonl")
		if err := j.Flush(path); err != nil {
			t.Fatalf("n=%d: Flush 失败: %v", n, err)
		}
		got, err := Replay(path)
		if err != nil {
			t.Fatalf("n=%d: Replay 失败: %v", n, err)
		}
		if len(got) != n {
			t.Fatalf("n=%d: 回放条数 = %d, 期望 %d", n, len(got), n)
		}
		for i := range got {
			if got[i].Seq != i+1 {
				t.Fatalf("n=%d: 第 %d 条序号 = %d, 期望 %d", n, i, got[i].Seq, i+1)
			}
		}
	}
}

// TestFlushWritesNonEmptyFileForSmallJournal 断言少量流水也会真正写入文件。
func TestFlushWritesNonEmptyFileForSmallJournal(t *testing.T) {
	j := NewJournal()
	fill(j, 3)
	path := filepath.Join(t.TempDir(), "small.jsonl")
	if err := j.Flush(path); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("Flush 后流水文件为空, 期望包含 3 条记录")
	}
}

// TestFlushOverwritesPreviousContent 断言重复落盘不会残留旧内容。
func TestFlushOverwritesPreviousContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.jsonl")

	first := NewJournal()
	fill(first, 12)
	if err := first.Flush(path); err != nil {
		t.Fatalf("首次 Flush 失败: %v", err)
	}

	second := NewJournal()
	fill(second, 2)
	if err := second.Flush(path); err != nil {
		t.Fatalf("二次 Flush 失败: %v", err)
	}

	got, err := Replay(path)
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("覆盖后回放条数 = %d, 期望 2", len(got))
	}
}

// TestFlushCreatesMissingDirectory 断言落盘会自动创建目录。
func TestFlushCreatesMissingDirectory(t *testing.T) {
	j := NewJournal()
	fill(j, 4)
	path := filepath.Join(t.TempDir(), "nested", "deep", "journal.jsonl")
	if err := j.Flush(path); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	got, err := Replay(path)
	if err != nil {
		t.Fatalf("Replay 失败: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("回放条数 = %d, 期望 4", len(got))
	}
}

func TestFlushRejectsEmptyPath(t *testing.T) {
	j := NewJournal()
	fill(j, 1)
	if err := j.Flush(""); err == nil {
		t.Fatalf("空路径应返回错误")
	}
}

func TestFailuresFilter(t *testing.T) {
	j := NewJournal()
	fill(j, 9)
	got := j.Failures()
	if len(got) != 3 {
		t.Fatalf("失败条数 = %d, 期望 3", len(got))
	}
	for _, e := range got {
		if e.Success {
			t.Fatalf("Failures 返回了成功条目: %+v", e)
		}
	}
}

// TestSaveJSONPersistsPayload 断言 SaveJSON 返回后文件内容完整。
func TestSaveJSONPersistsPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "summary.json")
	payload := map[string]any{"port": "zhapo", "approved": 18, "zone": "nanhai-north"}
	if err := SaveJSON(path, payload); err != nil {
		t.Fatalf("SaveJSON 失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("SaveJSON 后文件为空")
	}
}

func TestReplayMissingFile(t *testing.T) {
	if _, err := Replay(filepath.Join(t.TempDir(), "absent.jsonl")); err == nil {
		t.Fatalf("读取不存在的文件应返回错误")
	}
}
