package report

import (
	"testing"
	"time"

	"nanhaiport/internal/model"
	"nanhaiport/internal/permit"
	"nanhaiport/internal/quota"
	"nanhaiport/internal/season"
	"nanhaiport/internal/seed"
)

func newBuilder(t *testing.T) *Builder {
	t.Helper()
	reg, ledger, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load 失败: %v", err)
	}
	cal := season.NewCalendar()
	return NewBuilder(reg, cal, ledger, permit.NewService(reg, cal, ledger))
}

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("解析时间 %q 失败: %v", value, err)
	}
	return parsed
}

// TestSeasonReportTotalQuotaMatchesLedger 断言休渔报表中的配额总量与台账登记总量一致。
func TestSeasonReportTotalQuotaMatchesLedger(t *testing.T) {
	b := newBuilder(t)
	rep, err := b.Season(at(t, "2026-08-18T09:00:00+08:00"))
	if err != nil {
		t.Fatalf("Season 失败: %v", err)
	}
	want := seed.TotalAllocatedTonnes()
	if rep.TotalQuota != want {
		t.Errorf("报表配额合计 = %.2f, 期望 %.2f", rep.TotalQuota, want)
	}
	if rep.QuotaSummary.TotalQuota != want {
		t.Errorf("台账汇总配额合计 = %.2f, 期望 %.2f", rep.QuotaSummary.TotalQuota, want)
	}
	if rep.QuotaSummary.Allocations != len(seed.Allocations()) {
		t.Errorf("台账配额条数 = %d, 期望 %d", rep.QuotaSummary.Allocations, len(seed.Allocations()))
	}
}

// TestSeasonReportIsRepeatable 断言连续生成两次休渔报表结果完全一致。
func TestSeasonReportIsRepeatable(t *testing.T) {
	b := newBuilder(t)
	when := at(t, "2026-08-18T09:00:00+08:00")
	first, err := b.Season(when)
	if err != nil {
		t.Fatalf("首次 Season 失败: %v", err)
	}
	second, err := b.Season(when)
	if err != nil {
		t.Fatalf("二次 Season 失败: %v", err)
	}
	if first.TotalQuota != second.TotalQuota {
		t.Fatalf("两次报表配额合计不一致: %.2f vs %.2f", first.TotalQuota, second.TotalQuota)
	}
	if len(first.Ports) != len(second.Ports) {
		t.Fatalf("两次报表渔港行数不一致: %d vs %d", len(first.Ports), len(second.Ports))
	}
	for i := range first.Ports {
		if first.Ports[i] != second.Ports[i] {
			t.Fatalf("渔港 %s 两次报表不一致: %+v vs %+v", first.Ports[i].PortCode, first.Ports[i], second.Ports[i])
		}
	}
}

// TestSeasonReportPortQuotaSumsToTotal 断言各渔港配额之和等于报表合计。
func TestSeasonReportPortQuotaSumsToTotal(t *testing.T) {
	b := newBuilder(t)
	rep, err := b.Season(at(t, "2026-08-18T09:00:00+08:00"))
	if err != nil {
		t.Fatalf("Season 失败: %v", err)
	}
	var sum float64
	for _, line := range rep.Ports {
		sum += line.QuotaTonnes
	}
	if sum != rep.TotalQuota {
		t.Fatalf("渔港配额之和 = %.2f, 报表合计 = %.2f", sum, rep.TotalQuota)
	}
	if sum != seed.TotalAllocatedTonnes() {
		t.Fatalf("渔港配额之和 = %.2f, 台账登记 = %.2f", sum, seed.TotalAllocatedTonnes())
	}
}

func TestSeasonReportPhases(t *testing.T) {
	b := newBuilder(t)
	rep, err := b.Season(at(t, "2026-06-15T09:00:00+08:00"))
	if err != nil {
		t.Fatalf("Season 失败: %v", err)
	}
	phases := map[string]string{}
	for _, line := range rep.Ports {
		phases[line.PortCode] = line.Phase
	}
	if phases["zhapo"] != string(season.PhaseClosed) {
		t.Errorf("闸坡渔港 phase = %s, 期望 closed", phases["zhapo"])
	}
	if phases["sanya"] != string(season.PhaseExempt) {
		t.Errorf("三亚渔港 phase = %s, 期望 exempt", phases["sanya"])
	}
}

func TestVesselReport(t *testing.T) {
	b := newBuilder(t)
	line, err := b.Vessel(at(t, "2026-06-15T09:00:00+08:00"), "YJ-001")
	if err != nil {
		t.Fatalf("Vessel 失败: %v", err)
	}
	if line.MayDepart {
		t.Errorf("休渔期内拖网船不应允许出港")
	}
	if line.PowerTier != "large" {
		t.Errorf("功率档 = %s, 期望 large", line.PowerTier)
	}
	if line.QuotaTonnes != 208 {
		t.Errorf("配额合计 = %.2f, 期望 208", line.QuotaTonnes)
	}
	if len(line.Usage) != 2 {
		t.Errorf("使用情况条数 = %d, 期望 2", len(line.Usage))
	}

	exempt, err := b.Vessel(at(t, "2026-06-15T09:00:00+08:00"), "YJ-004")
	if err != nil {
		t.Fatalf("Vessel 失败: %v", err)
	}
	if !exempt.MayDepart {
		t.Errorf("单船钓船休渔期内应允许出港")
	}
}

func TestZoneReport(t *testing.T) {
	b := newBuilder(t)
	lines, err := b.Zones(at(t, "2026-08-16T12:00:00+08:00"))
	if err != nil {
		t.Fatalf("Zones 失败: %v", err)
	}
	if len(lines) != len(model.AllZones()) {
		t.Fatalf("海区行数 = %d, 期望 %d", len(lines), len(model.AllZones()))
	}
	byZone := map[string]ZoneLine{}
	for _, l := range lines {
		byZone[l.Zone] = l
	}
	if got := byZone[string(model.ZoneNanhaiNorth)]; got.Phase != string(season.PhaseOpen) {
		t.Errorf("南海北部在开渔时刻 phase = %s, 期望 open", got.Phase)
	}
	if got := byZone[string(model.ZoneQiongzhou)]; got.Phase != string(season.PhaseOpen) {
		t.Errorf("琼州海峡 phase = %s, 期望 open", got.Phase)
	}
	if got := byZone[string(model.ZoneNanhaiSouth)]; got.Phase != string(season.PhaseExempt) {
		t.Errorf("南海南部 phase = %s, 期望 exempt", got.Phase)
	}
}

func TestZoneReportQuotaTotals(t *testing.T) {
	b := newBuilder(t)
	lines, err := b.Zones(at(t, "2026-08-18T09:00:00+08:00"))
	if err != nil {
		t.Fatalf("Zones 失败: %v", err)
	}
	var sum float64
	for _, l := range lines {
		sum += l.QuotaTonnes
	}
	if sum != seed.TotalAllocatedTonnes() {
		t.Fatalf("海区配额之和 = %.2f, 期望 %.2f", sum, seed.TotalAllocatedTonnes())
	}
}

func TestVesselReportUnknown(t *testing.T) {
	b := newBuilder(t)
	if _, err := b.Vessel(time.Now(), "NOPE"); err == nil {
		t.Fatalf("未知渔船应返回错误")
	}
}

func TestQuotaUsageZeroLanded(t *testing.T) {
	b := newBuilder(t)
	line, err := b.Vessel(at(t, "2026-08-18T09:00:00+08:00"), "QZ-001")
	if err != nil {
		t.Fatalf("Vessel 失败: %v", err)
	}
	if len(line.Usage) != 1 {
		t.Fatalf("使用情况条数 = %d, 期望 1", len(line.Usage))
	}
	want := quota.Usage{VesselID: "QZ-001", Species: "虾类", Quota: 34, Landed: 0, Remaining: 34}
	if line.Usage[0] != want {
		t.Fatalf("使用情况 = %+v, 期望 %+v", line.Usage[0], want)
	}
}
