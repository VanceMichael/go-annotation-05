package model

import (
	"errors"
	"testing"
	"time"
)

func TestParseZone(t *testing.T) {
	for _, z := range AllZones() {
		got, err := ParseZone(string(z))
		if err != nil || got != z {
			t.Fatalf("ParseZone(%q) = %q, %v", z, got, err)
		}
	}
	if _, err := ParseZone("  NANHAI-NORTH "); err != nil {
		t.Fatalf("ParseZone 应忽略大小写与空白: %v", err)
	}
	if _, err := ParseZone("donghai"); !errors.Is(err, ErrUnknownZone) {
		t.Fatalf("未知海区应返回 ErrUnknownZone, 实际 %v", err)
	}
}

func TestParseVesselClass(t *testing.T) {
	for _, c := range AllClasses() {
		got, err := ParseVesselClass(string(c))
		if err != nil || got != c {
			t.Fatalf("ParseVesselClass(%q) = %q, %v", c, got, err)
		}
	}
	if _, err := ParseVesselClass("longline"); !errors.Is(err, ErrUnknownVesselClass) {
		t.Fatalf("未知作业类型应返回 ErrUnknownVesselClass, 实际 %v", err)
	}
}

func TestExemptFromClosure(t *testing.T) {
	exempt := map[VesselClass]bool{ClassSingleHook: true, ClassFixedNet: true}
	for _, c := range AllClasses() {
		if got := c.ExemptFromClosure(); got != exempt[c] {
			t.Errorf("%s.ExemptFromClosure() = %v, 期望 %v", c, got, exempt[c])
		}
	}
}

func TestVesselValidate(t *testing.T) {
	base := Vessel{
		ID: "YJ-001", Name: "粤阳江渔00121", HomePort: "zhapo",
		Class: ClassTrawler, PowerKW: 300, CrewSize: 12,
		Registered: time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("合法渔船不应报错: %v", err)
	}

	mutations := []func(v *Vessel){
		func(v *Vessel) { v.ID = "" },
		func(v *Vessel) { v.Name = " " },
		func(v *Vessel) { v.HomePort = "" },
		func(v *Vessel) { v.Class = "longline" },
		func(v *Vessel) { v.PowerKW = 0 },
		func(v *Vessel) { v.CrewSize = -1 },
		func(v *Vessel) { v.Registered = time.Time{} },
	}
	for i, mutate := range mutations {
		v := base
		mutate(&v)
		if err := v.Validate(); !errors.Is(err, ErrInvalidVessel) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidVessel, 实际 %v", i, err)
		}
	}
}

func TestPowerTier(t *testing.T) {
	cases := map[float64]string{20: "small", 43.9: "small", 44: "medium", 249: "medium", 250: "large", 480: "large"}
	for power, want := range cases {
		v := Vessel{PowerKW: power}
		if got := v.PowerTier(); got != want {
			t.Errorf("PowerKW=%.1f PowerTier = %s, 期望 %s", power, got, want)
		}
	}
}

func TestPermitStateTerminal(t *testing.T) {
	terminal := map[PermitState]bool{StateRejected: true, StateClosed: true}
	states := []PermitState{StateDraft, StateSubmitted, StateReviewed, StateApproved, StateRejected, StateSuspended, StateClosed}
	for _, s := range states {
		if got := s.Terminal(); got != terminal[s] {
			t.Errorf("%s.Terminal() = %v, 期望 %v", s, got, terminal[s])
		}
	}
}

func TestPermitDurationAndHistory(t *testing.T) {
	depart := time.Date(2026, time.August, 18, 5, 0, 0, 0, time.UTC)
	p := Permit{DepartAt: depart, ReturnBy: depart.Add(72 * time.Hour)}
	if got := p.Duration(); got != 72*time.Hour {
		t.Errorf("Duration = %v, 期望 72h", got)
	}
	if _, ok := p.LastTransition(); ok {
		t.Errorf("空历史不应返回流转记录")
	}
	empty := Permit{}
	if got := empty.Duration(); got != 0 {
		t.Errorf("缺少时间的许可 Duration = %v, 期望 0", got)
	}
	p.History = []Transition{{From: StateDraft, To: StateSubmitted}, {From: StateSubmitted, To: StateReviewed}}
	last, ok := p.LastTransition()
	if !ok || last.To != StateReviewed {
		t.Errorf("LastTransition = %+v, %v", last, ok)
	}
}

func TestCatchRecordValidate(t *testing.T) {
	base := CatchRecord{
		VesselID: "YJ-001", PortCode: "zhapo", Species: "带鱼", Tonnage: 12.5,
		LandedAt: time.Date(2026, time.August, 18, 6, 0, 0, 0, time.UTC),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("合法记录不应报错: %v", err)
	}
	mutations := []func(c *CatchRecord){
		func(c *CatchRecord) { c.VesselID = "" },
		func(c *CatchRecord) { c.Species = "" },
		func(c *CatchRecord) { c.Tonnage = 0 },
		func(c *CatchRecord) { c.LandedAt = time.Time{} },
	}
	for i, mutate := range mutations {
		c := base
		mutate(&c)
		if err := c.Validate(); !errors.Is(err, ErrInvalidCatch) {
			t.Errorf("第 %d 项非法输入应返回 ErrInvalidCatch, 实际 %v", i, err)
		}
	}
}

func TestSortCatchRecords(t *testing.T) {
	t0 := time.Date(2026, time.August, 18, 6, 0, 0, 0, time.UTC)
	records := []CatchRecord{
		{VesselID: "YJ-002", Species: "带鱼", LandedAt: t0.Add(time.Hour)},
		{VesselID: "YJ-001", Species: "金线鱼", LandedAt: t0},
		{VesselID: "YJ-001", Species: "带鱼", LandedAt: t0},
	}
	SortCatchRecords(records)
	if records[0].Species != "带鱼" || records[0].VesselID != "YJ-001" {
		t.Fatalf("排序结果首条 = %+v", records[0])
	}
	if records[2].VesselID != "YJ-002" {
		t.Fatalf("排序结果末条 = %+v", records[2])
	}
}

func TestDisplayNames(t *testing.T) {
	if ZoneNanhaiNorth.DisplayName() != "南海北部" {
		t.Errorf("海区名称异常: %s", ZoneNanhaiNorth.DisplayName())
	}
	if Zone("unknown").DisplayName() != "unknown" {
		t.Errorf("未知海区应回落为原值")
	}
	if ClassTrawler.DisplayName() != "拖网" {
		t.Errorf("作业类型名称异常: %s", ClassTrawler.DisplayName())
	}
	if VesselClass("x").DisplayName() != "x" {
		t.Errorf("未知作业类型应回落为原值")
	}
	if StateApproved.DisplayName() != "已核准" {
		t.Errorf("状态名称异常: %s", StateApproved.DisplayName())
	}
	if PermitState("x").DisplayName() != "x" {
		t.Errorf("未知状态应回落为原值")
	}
}
