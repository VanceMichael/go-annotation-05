package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func run(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	var payload map[string]any
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			// 非 JSON 输出（例如 version / help）时保持 payload 为 nil。
			payload = nil
		}
	}
	return code, payload, stderr.String()
}

func TestHelpAndVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(nil, &stdout, &stderr); code != ExitOK {
		t.Fatalf("无参数退出码 = %d, 期望 %d", code, ExitOK)
	}
	if stdout.Len() == 0 {
		t.Fatalf("无参数应输出用法说明")
	}
	stdout.Reset()
	if code := Run([]string{"version"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("version 退出码 = %d", code)
	}
	if !bytes.Contains(stdout.Bytes(), []byte(Version)) {
		t.Fatalf("version 输出 = %q, 期望包含 %q", stdout.String(), Version)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, _ := run(t, "teleport")
	if code != ExitUsage {
		t.Fatalf("未知命令退出码 = %d, 期望 %d", code, ExitUsage)
	}
}

// TestSeasonStatusAtReopeningInstant 断言开渔时刻命令行报告已开渔。
func TestSeasonStatusAtReopeningInstant(t *testing.T) {
	code, payload, stderr := run(t, "season", "status", "--zone", "nanhai-north", "--at", "2026-08-16T12:00:00+08:00")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if payload["phase"] != "open" {
		t.Fatalf("phase = %v, 期望 open (输出: %+v)", payload["phase"], payload)
	}
	if closed, _ := payload["closed"].(bool); closed {
		t.Fatalf("closed = true, 期望 false")
	}
}

func TestSeasonStatusDuringClosure(t *testing.T) {
	code, payload, stderr := run(t, "season", "status", "--zone", "nanhai-north", "--at", "2026-06-20T09:00:00+08:00")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if payload["phase"] != "closed" {
		t.Fatalf("phase = %v, 期望 closed", payload["phase"])
	}
}

func TestSeasonStatusUnknownZone(t *testing.T) {
	code, _, _ := run(t, "season", "status", "--zone", "donghai")
	if code != ExitBadRequest {
		t.Fatalf("未知海区退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

func TestSeasonStatusBadTime(t *testing.T) {
	code, _, _ := run(t, "season", "status", "--at", "2026/08/16")
	if code != ExitBadRequest {
		t.Fatalf("非法时间退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}

// TestPermitRunDuringClosureExitsConflict 断言休渔期内跑完整许可流程返回业务冲突退出码 3。
func TestPermitRunDuringClosureExitsConflict(t *testing.T) {
	code, payload, stderr := run(t, "permit", "run", "--vessel", "YJ-001", "--depart", "2026-06-20T05:00:00+08:00")
	if code != ExitConflict {
		t.Fatalf("退出码 = %d, 期望 %d (业务冲突); stdout=%+v stderr=%s", code, ExitConflict, payload, stderr)
	}
	if payload != nil {
		if ok, _ := payload["ok"].(bool); ok {
			t.Fatalf("休渔期内许可不应核准通过: %+v", payload)
		}
	}
}

// TestPermitRunUnknownVesselExitsNotFound 断言渔船未登记返回退出码 5。
func TestPermitRunUnknownVesselExitsNotFound(t *testing.T) {
	code, _, _ := run(t, "permit", "run", "--vessel", "NOPE-1", "--depart", "2026-08-20T05:00:00+08:00")
	if code != ExitNotFound {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitNotFound)
	}
}

// TestPermitRunAtReopeningInstantSucceeds 断言开渔时刻的许可流程可以走完。
func TestPermitRunAtReopeningInstantSucceeds(t *testing.T) {
	code, payload, stderr := run(t, "permit", "run", "--vessel", "YJ-001", "--depart", "2026-08-16T12:00:00+08:00")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	if payload["state"] != "approved" {
		t.Fatalf("许可状态 = %v, 期望 approved", payload["state"])
	}
}

func TestPermitRunExemptClassDuringClosure(t *testing.T) {
	code, payload, stderr := run(t, "permit", "run", "--vessel", "YJ-004", "--depart", "2026-06-20T05:00:00+08:00")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
}

// TestQuotaAuditKeepsLedgerIntact 断言按渔船审计配额后台账汇总保持完整。
func TestQuotaAuditKeepsLedgerIntact(t *testing.T) {
	code, payload, stderr := run(t, "quota", "audit", "--vessel", "YJ-001")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	summary, ok := payload["ledger_summary"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 ledger_summary: %+v", payload)
	}
	if got, _ := summary["allocations"].(float64); got != 17 {
		t.Fatalf("审计后台账配额条数 = %v, 期望 17", summary["allocations"])
	}
	if got, _ := summary["vessels"].(float64); got != 14 {
		t.Fatalf("审计后台账渔船数 = %v, 期望 14", summary["vessels"])
	}
	vessels, _ := payload["ledger_vessels"].([]any)
	if len(vessels) != 14 {
		t.Fatalf("审计后台账渔船列表长度 = %d, 期望 14", len(vessels))
	}
}

// TestSelfcheckPasses 断言内置自检全部通过。
func TestSelfcheckPasses(t *testing.T) {
	code, payload, stderr := run(t, "selfcheck")
	if code != ExitOK {
		t.Fatalf("自检退出码 = %d, 期望 0; stdout=%+v stderr=%s", code, payload, stderr)
	}
	if failed, _ := payload["failed"].(float64); failed != 0 {
		t.Fatalf("自检失败项 = %v, 期望 0; 输出 %+v", payload["failed"], payload)
	}
}

// TestReportSeasonTotals 断言休渔报表命令输出的配额合计正确。
func TestReportSeasonTotals(t *testing.T) {
	code, payload, stderr := run(t, "report", "season", "--at", "2026-08-18T09:00:00+08:00")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if got, _ := payload["total_quota"].(float64); got != 1681 {
		t.Fatalf("报表 total_quota = %v, 期望 1681", payload["total_quota"])
	}
}

func TestReportZones(t *testing.T) {
	code, payload, stderr := run(t, "report", "zones", "--at", "2026-08-16T12:00:00+08:00")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	zones, ok := payload["zones"].([]any)
	if !ok || len(zones) != 4 {
		t.Fatalf("zones = %+v", payload["zones"])
	}
}

func TestReportSeasonWritesFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nested", "season.json")
	code, _, stderr := run(t, "report", "season", "--at", "2026-08-18T09:00:00+08:00", "--out", out)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if _, err := readJSONFile(out); err != nil {
		t.Fatalf("报表文件不可读: %v", err)
	}
}

// TestDispatchRunHonoursTimeout 断言批量调度在超时后立即中止并返回退出码 4。
func TestDispatchRunHonoursTimeout(t *testing.T) {
	code, payload, stderr := run(t,
		"dispatch", "run",
		"--workers", "3",
		"--count", "12",
		"--timeout", "200ms",
		"--gateway-latency", "5s",
	)
	if code != ExitAborted {
		t.Fatalf("退出码 = %d, 期望 %d (调度中止); stdout=%+v stderr=%s", code, ExitAborted, payload, stderr)
	}
	if aborted, _ := payload["aborted"].(bool); !aborted {
		t.Fatalf("aborted = %v, 期望 true; 输出 %+v", payload["aborted"], payload)
	}
	if elapsed, _ := payload["elapsed_ms"].(float64); elapsed > 2000 {
		t.Fatalf("超时后耗时 = %v ms, 期望远小于 2000 ms", elapsed)
	}
}

func TestDispatchRunCompletes(t *testing.T) {
	code, payload, stderr := run(t,
		"dispatch", "run",
		"--workers", "4",
		"--count", "8",
		"--timeout", "20s",
		"--gateway-latency", "1ms",
	)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if done, _ := payload["done"].(float64); done != 8 {
		t.Fatalf("done = %v, 期望 8", payload["done"])
	}
}

// TestDispatchRunFlushesJournal 断言调度流水落盘后可以完整回放。
func TestDispatchRunFlushesJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	code, payload, stderr := run(t,
		"dispatch", "run",
		"--workers", "2",
		"--count", "6",
		"--timeout", "20s",
		"--gateway-latency", "1ms",
		"--journal", path,
	)
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stdout=%+v stderr=%s", code, payload, stderr)
	}
	if got, _ := payload["journal_len"].(float64); got != 6 {
		t.Fatalf("journal_len = %v, 期望 6", payload["journal_len"])
	}

	code, replay, stderr := run(t, "journal", "replay", "--path", path)
	if code != ExitOK {
		t.Fatalf("回放退出码 = %d, stderr=%s", code, stderr)
	}
	if got, _ := replay["entries"].(float64); got != 6 {
		t.Fatalf("回放条数 = %v, 期望 6", replay["entries"])
	}
}

func TestVesselShowAndList(t *testing.T) {
	code, payload, stderr := run(t, "vessel", "show", "--id", "YJ-001", "--at", "2026-06-15T09:00:00+08:00")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if mayDepart, _ := payload["may_depart"].(bool); mayDepart {
		t.Fatalf("休渔期内拖网船 may_depart = true, 期望 false")
	}

	code, payload, stderr = run(t, "vessel", "list", "--port", "zhapo")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if vessels, _ := payload["vessels"].([]any); len(vessels) != 4 {
		t.Fatalf("闸坡渔港渔船数 = %d, 期望 4", len(vessels))
	}

	code, _, _ = run(t, "vessel", "list", "--port", "nope")
	if code != ExitNotFound {
		t.Fatalf("未知渔港退出码 = %d, 期望 %d", code, ExitNotFound)
	}
}

func TestPortListAndQuotaSummary(t *testing.T) {
	code, payload, stderr := run(t, "port", "list")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if ports, _ := payload["ports"].([]any); len(ports) != 6 {
		t.Fatalf("渔港数 = %d, 期望 6", len(ports))
	}

	code, payload, stderr = run(t, "quota", "summary")
	if code != ExitOK {
		t.Fatalf("退出码 = %d, stderr=%s", code, stderr)
	}
	if got, _ := payload["total_quota"].(float64); got != 1681 {
		t.Fatalf("total_quota = %v, 期望 1681", payload["total_quota"])
	}
}

func TestJournalReplayMissingArgs(t *testing.T) {
	code, _, _ := run(t, "journal", "replay")
	if code != ExitBadRequest {
		t.Fatalf("退出码 = %d, 期望 %d", code, ExitBadRequest)
	}
}
