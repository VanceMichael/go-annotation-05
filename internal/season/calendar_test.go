package season

import (
	"errors"
	"testing"
	"time"

	"nanhaiport/internal/model"
)

func bj(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("解析时间 %q 失败: %v", value, err)
	}
	return parsed
}

func TestWindowForZones(t *testing.T) {
	cases := []struct {
		zone      model.Zone
		wantStart string
		wantEnd   string
	}{
		{model.ZoneNanhaiNorth, "2026-05-01T12:00:00+08:00", "2026-08-16T12:00:00+08:00"},
		{model.ZoneBeibuGulf, "2026-05-01T12:00:00+08:00", "2026-08-16T12:00:00+08:00"},
		{model.ZoneQiongzhou, "2026-05-01T12:00:00+08:00", "2026-08-01T12:00:00+08:00"},
	}
	for _, tc := range cases {
		w, err := WindowFor(2026, tc.zone)
		if err != nil {
			t.Fatalf("%s: WindowFor 返回错误: %v", tc.zone, err)
		}
		if !w.Start.Equal(bj(t, tc.wantStart)) {
			t.Errorf("%s: 起始时刻 = %s, 期望 %s", tc.zone, w.Start, tc.wantStart)
		}
		if !w.End.Equal(bj(t, tc.wantEnd)) {
			t.Errorf("%s: 结束时刻 = %s, 期望 %s", tc.zone, w.End, tc.wantEnd)
		}
	}
}

func TestWindowForExemptZone(t *testing.T) {
	if _, err := WindowFor(2026, model.ZoneNanhaiSouth); !errors.Is(err, model.ErrNoSeasonWindow) {
		t.Fatalf("南海南部应返回 ErrNoSeasonWindow, 实际 %v", err)
	}
}

func TestWindowForUnknownZone(t *testing.T) {
	if _, err := WindowFor(2026, model.Zone("dongshan")); !errors.Is(err, model.ErrUnknownZone) {
		t.Fatalf("未知海区应返回 ErrUnknownZone, 实际 %v", err)
	}
}

// TestWindowContainsAroundReopening 断言开渔时刻的归属：
// 开渔时刻之前属于休渔期，开渔时刻本身及之后不属于休渔期。
func TestWindowContainsAroundReopening(t *testing.T) {
	w, err := WindowFor(2026, model.ZoneNanhaiNorth)
	if err != nil {
		t.Fatalf("WindowFor 返回错误: %v", err)
	}
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"开渔前一小时", bj(t, "2026-08-16T11:00:00+08:00"), true},
		{"开渔前一秒", bj(t, "2026-08-16T11:59:59+08:00"), true},
		{"开渔时刻", bj(t, "2026-08-16T12:00:00+08:00"), false},
		{"开渔后一秒", bj(t, "2026-08-16T12:00:01+08:00"), false},
		{"开渔当天下午", bj(t, "2026-08-16T18:30:00+08:00"), false},
		{"休渔起始时刻", bj(t, "2026-05-01T12:00:00+08:00"), true},
		{"休渔起始前一秒", bj(t, "2026-05-01T11:59:59+08:00"), false},
	}
	for _, tc := range cases {
		if got := w.Contains(tc.at); got != tc.want {
			t.Errorf("%s (%s): Contains = %v, 期望 %v", tc.name, tc.at.Format(time.RFC3339), got, tc.want)
		}
	}
}

// TestCalendarStatusAtReopeningInstant 断言开渔时刻的休渔状态为已开渔。
func TestCalendarStatusAtReopeningInstant(t *testing.T) {
	cal := NewCalendar()
	cases := []struct {
		name      string
		at        string
		wantPhase Phase
	}{
		{"休渔前", "2026-04-20T09:00:00+08:00", PhaseBeforeClosure},
		{"休渔中", "2026-06-15T09:00:00+08:00", PhaseClosed},
		{"开渔前一分钟", "2026-08-16T11:59:00+08:00", PhaseClosed},
		{"开渔时刻", "2026-08-16T12:00:00+08:00", PhaseOpen},
		{"开渔当天傍晚", "2026-08-16T19:00:00+08:00", PhaseOpen},
		{"开渔次日", "2026-08-17T08:00:00+08:00", PhaseOpen},
	}
	for _, tc := range cases {
		st, err := cal.StatusAt(bj(t, tc.at), model.ZoneNanhaiNorth)
		if err != nil {
			t.Fatalf("%s: StatusAt 返回错误: %v", tc.name, err)
		}
		if st.Phase != tc.wantPhase {
			t.Errorf("%s (%s): phase = %s, 期望 %s", tc.name, tc.at, st.Phase, tc.wantPhase)
		}
	}
}

// TestCalendarMayDepartAtReopeningInstant 断言开渔时刻起拖网船可以出港。
func TestCalendarMayDepartAtReopeningInstant(t *testing.T) {
	cal := NewCalendar()
	at := bj(t, "2026-08-16T12:00:00+08:00")
	ok, err := cal.MayDepart(at, model.ZoneNanhaiNorth, model.ClassTrawler)
	if err != nil {
		t.Fatalf("MayDepart 返回错误: %v", err)
	}
	if !ok {
		t.Fatalf("2026-08-16 12:00 已到开渔时刻, 拖网船应允许出港, 实际被拒绝")
	}
}

func TestCalendarMayDepartExemptClass(t *testing.T) {
	cal := NewCalendar()
	at := bj(t, "2026-06-01T09:00:00+08:00")
	for _, class := range []model.VesselClass{model.ClassSingleHook, model.ClassFixedNet} {
		ok, err := cal.MayDepart(at, model.ZoneNanhaiNorth, class)
		if err != nil {
			t.Fatalf("%s: MayDepart 返回错误: %v", class, err)
		}
		if !ok {
			t.Errorf("%s 不受休渔限制, 应允许出港", class)
		}
	}
	ok, err := cal.MayDepart(at, model.ZoneNanhaiNorth, model.ClassTrawler)
	if err != nil {
		t.Fatalf("MayDepart 返回错误: %v", err)
	}
	if ok {
		t.Errorf("休渔期内拖网船应禁止出港")
	}
}

func TestCalendarStatusExemptZone(t *testing.T) {
	cal := NewCalendar()
	st, err := cal.StatusAt(bj(t, "2026-06-01T09:00:00+08:00"), model.ZoneNanhaiSouth)
	if err != nil {
		t.Fatalf("StatusAt 返回错误: %v", err)
	}
	if st.Phase != PhaseExempt {
		t.Fatalf("南海南部 phase = %s, 期望 %s", st.Phase, PhaseExempt)
	}
}

func TestRemainingDaysCeil(t *testing.T) {
	cal := NewCalendar()
	cases := []struct {
		at   string
		want int
	}{
		{"2026-08-15T12:00:00+08:00", 1},
		{"2026-08-15T20:00:00+08:00", 1},
		{"2026-08-16T11:00:00+08:00", 1},
		{"2026-08-14T12:00:00+08:00", 2},
	}
	for _, tc := range cases {
		st, err := cal.StatusAt(bj(t, tc.at), model.ZoneNanhaiNorth)
		if err != nil {
			t.Fatalf("StatusAt 返回错误: %v", err)
		}
		if st.RemainingDays != tc.want {
			t.Errorf("%s: 剩余天数 = %d, 期望 %d", tc.at, st.RemainingDays, tc.want)
		}
	}
}

func TestNextOpening(t *testing.T) {
	cal := NewCalendar()
	got, err := cal.NextOpening(bj(t, "2026-06-01T09:00:00+08:00"), model.ZoneNanhaiNorth)
	if err != nil {
		t.Fatalf("NextOpening 返回错误: %v", err)
	}
	if !got.Equal(bj(t, "2026-08-16T12:00:00+08:00")) {
		t.Errorf("下一次开渔时刻 = %s, 期望 2026-08-16 12:00 +08:00", got)
	}

	got, err = cal.NextOpening(bj(t, "2026-09-01T09:00:00+08:00"), model.ZoneNanhaiNorth)
	if err != nil {
		t.Fatalf("NextOpening 返回错误: %v", err)
	}
	if !got.Equal(bj(t, "2027-08-16T12:00:00+08:00")) {
		t.Errorf("跨年开渔时刻 = %s, 期望 2027-08-16 12:00 +08:00", got)
	}
}

func TestWindowFormatAndLength(t *testing.T) {
	w, err := WindowFor(2026, model.ZoneQiongzhou)
	if err != nil {
		t.Fatalf("WindowFor 返回错误: %v", err)
	}
	if w.Length() != 92*24*time.Hour {
		t.Errorf("琼州海峡休渔时长 = %v, 期望 %v", w.Length(), 92*24*time.Hour)
	}
	want := "琼州海峡 2026-05-01 12:00 至 2026-08-01 12:00"
	if got := w.Format(); got != want {
		t.Errorf("Format = %q, 期望 %q", got, want)
	}
}
