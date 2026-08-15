package quota

import (
	"errors"
	"testing"
	"time"

	"nanhaiport/internal/model"
)

func seedLedger(t *testing.T) *Ledger {
	t.Helper()
	l := NewLedger()
	rows := []Allocation{
		{VesselID: "YJ-001", Species: "带鱼", Tonnes: 120},
		{VesselID: "YJ-001", Species: "金线鱼", Tonnes: 80},
		{VesselID: "YJ-002", Species: "带鱼", Tonnes: 95},
		{VesselID: "YJ-003", Species: "蓝圆鲹", Tonnes: 150},
		{VesselID: "YJ-003", Species: "带鱼", Tonnes: 60},
		{VesselID: "BH-001", Species: "二长棘鲷", Tonnes: 45},
	}
	for _, a := range rows {
		if err := l.Allocate(a); err != nil {
			t.Fatalf("Allocate(%+v) 失败: %v", a, err)
		}
	}
	return l
}

func TestAllocateAccumulates(t *testing.T) {
	l := NewLedger()
	if err := l.Allocate(Allocation{VesselID: "YJ-001", Species: "带鱼", Tonnes: 50}); err != nil {
		t.Fatalf("Allocate 失败: %v", err)
	}
	if err := l.Allocate(Allocation{VesselID: "YJ-001", Species: "带鱼", Tonnes: 30}); err != nil {
		t.Fatalf("Allocate 失败: %v", err)
	}
	if got := l.TotalQuotaFor("YJ-001"); got != 80 {
		t.Fatalf("累加后配额 = %.2f, 期望 80", got)
	}
	if got := len(l.Allocations()); got != 1 {
		t.Fatalf("配额条数 = %d, 期望 1", got)
	}
}

func TestAllocateRejectsInvalidInput(t *testing.T) {
	l := NewLedger()
	bad := []Allocation{
		{Species: "带鱼", Tonnes: 10},
		{VesselID: "YJ-001", Tonnes: 10},
		{VesselID: "YJ-001", Species: "带鱼", Tonnes: 0},
		{VesselID: "YJ-001", Species: "带鱼", Tonnes: -3},
	}
	for _, a := range bad {
		if err := l.Allocate(a); err == nil {
			t.Errorf("Allocate(%+v) 应返回错误", a)
		}
	}
}

// TestSummaryStableAfterVesselQuery 断言按渔船查询配额不会改变台账整体汇总。
func TestSummaryStableAfterVesselQuery(t *testing.T) {
	l := seedLedger(t)
	before := l.Summary()
	if before.Allocations != 6 || before.Vessels != 4 {
		t.Fatalf("初始汇总 = %+v, 期望 6 条配额 / 4 条渔船", before)
	}

	if got := l.VesselAllocations("YJ-001"); len(got) != 2 {
		t.Fatalf("YJ-001 配额条数 = %d, 期望 2", len(got))
	}

	after := l.Summary()
	if after != before {
		t.Fatalf("按渔船查询后汇总发生变化: 查询前 %+v, 查询后 %+v", before, after)
	}
}

// TestSummaryStableAfterSpeciesQuery 断言按鱼种查询配额不会改变台账整体汇总。
func TestSummaryStableAfterSpeciesQuery(t *testing.T) {
	l := seedLedger(t)
	before := l.Summary()

	if got := l.SpeciesAllocations("带鱼"); len(got) != 3 {
		t.Fatalf("带鱼配额条数 = %d, 期望 3", len(got))
	}

	after := l.Summary()
	if after != before {
		t.Fatalf("按鱼种查询后汇总发生变化: 查询前 %+v, 查询后 %+v", before, after)
	}
}

// TestVesselAllocationsRepeatable 断言同一查询重复执行结果一致。
func TestVesselAllocationsRepeatable(t *testing.T) {
	l := seedLedger(t)
	first := l.VesselAllocations("YJ-003")
	second := l.VesselAllocations("YJ-003")
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("两次查询条数 = %d / %d, 期望均为 2", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("第 %d 条不一致: %+v vs %+v", i, first[i], second[i])
		}
	}
}

// TestAllocationsIntactAfterMixedQueries 断言多次分组查询后全量配额清单保持完整。
func TestAllocationsIntactAfterMixedQueries(t *testing.T) {
	l := seedLedger(t)
	want := l.Allocations()

	l.VesselAllocations("YJ-001")
	l.SpeciesAllocations("带鱼")
	l.VesselAllocations("BH-001")

	got := l.Allocations()
	if len(got) != len(want) {
		t.Fatalf("查询后配额条数 = %d, 期望 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 条配额被改动: 期望 %+v, 实际 %+v", i, want[i], got[i])
		}
	}
}

func TestUsageAndCheckLanding(t *testing.T) {
	l := seedLedger(t)
	landedAt := time.Date(2026, time.August, 18, 6, 0, 0, 0, time.UTC)
	rec := model.CatchRecord{VesselID: "YJ-002", PortCode: "zhapo", Species: "带鱼", Tonnage: 40, LandedAt: landedAt}
	if err := l.CheckLanding(rec); err != nil {
		t.Fatalf("CheckLanding 应通过: %v", err)
	}
	if err := l.Land(rec); err != nil {
		t.Fatalf("Land 失败: %v", err)
	}

	usage := l.UsageFor("YJ-002")
	if len(usage) != 1 {
		t.Fatalf("使用情况条数 = %d, 期望 1", len(usage))
	}
	if usage[0].Landed != 40 || usage[0].Remaining != 55 || usage[0].Exceeded {
		t.Fatalf("使用情况 = %+v", usage[0])
	}

	over := model.CatchRecord{VesselID: "YJ-002", PortCode: "zhapo", Species: "带鱼", Tonnage: 60, LandedAt: landedAt}
	if err := l.CheckLanding(over); !errors.Is(err, model.ErrQuotaExceeded) {
		t.Fatalf("超限卸鱼应返回 ErrQuotaExceeded, 实际 %v", err)
	}

	noQuota := model.CatchRecord{VesselID: "YJ-002", PortCode: "zhapo", Species: "石斑鱼", Tonnage: 1, LandedAt: landedAt}
	if err := l.CheckLanding(noQuota); !errors.Is(err, model.ErrQuotaExceeded) {
		t.Fatalf("无配额鱼种应返回 ErrQuotaExceeded, 实际 %v", err)
	}
}

func TestSummaryTotals(t *testing.T) {
	l := seedLedger(t)
	landedAt := time.Date(2026, time.August, 18, 6, 0, 0, 0, time.UTC)
	if err := l.Land(model.CatchRecord{VesselID: "YJ-001", PortCode: "zhapo", Species: "带鱼", Tonnage: 130, LandedAt: landedAt}); err != nil {
		t.Fatalf("Land 失败: %v", err)
	}
	s := l.Summary()
	if s.TotalQuota != 550 {
		t.Errorf("配额总量 = %.2f, 期望 550", s.TotalQuota)
	}
	if s.TotalLanded != 130 {
		t.Errorf("卸鱼总量 = %.2f, 期望 130", s.TotalLanded)
	}
	if s.Exceeded != 1 {
		t.Errorf("超限条数 = %d, 期望 1", s.Exceeded)
	}
	if s.Species != 4 {
		t.Errorf("鱼种数 = %d, 期望 4", s.Species)
	}
}

func TestVesselIDs(t *testing.T) {
	l := seedLedger(t)
	got := l.VesselIDs()
	want := []string{"BH-001", "YJ-001", "YJ-002", "YJ-003"}
	if len(got) != len(want) {
		t.Fatalf("渔船列表 = %v, 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("渔船列表 = %v, 期望 %v", got, want)
		}
	}
}
