package permit

import (
	"context"
	"errors"
	"testing"
	"time"

	"nanhaiport/internal/model"
	"nanhaiport/internal/quota"
	"nanhaiport/internal/registry"
	"nanhaiport/internal/season"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("解析时间 %q 失败: %v", value, err)
	}
	return parsed
}

func newFixture(t *testing.T) (*Service, *registry.Registry, *quota.Ledger) {
	t.Helper()
	reg := registry.New()
	if err := reg.AddPort(model.Port{Code: "zhapo", Name: "闸坡渔港", Province: "广东", Zone: model.ZoneNanhaiNorth, Berths: 120}); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	vessels := []model.Vessel{
		{ID: "YJ-001", Name: "粤阳江渔00121", HomePort: "zhapo", Class: model.ClassTrawler, PowerKW: 280, CrewSize: 12, Registered: mustTime(t, "2018-04-01T00:00:00Z")},
		{ID: "YJ-009", Name: "粤阳江渔00909", HomePort: "zhapo", Class: model.ClassSingleHook, PowerKW: 60, CrewSize: 5, Registered: mustTime(t, "2020-06-01T00:00:00Z")},
	}
	for _, v := range vessels {
		if err := reg.AddVessel(v); err != nil {
			t.Fatalf("AddVessel 失败: %v", err)
		}
	}
	ledger := quota.NewLedger()
	for _, a := range []quota.Allocation{
		{VesselID: "YJ-001", Species: "带鱼", Tonnes: 120},
		{VesselID: "YJ-009", Species: "金线鱼", Tonnes: 30},
	} {
		if err := ledger.Allocate(a); err != nil {
			t.Fatalf("Allocate 失败: %v", err)
		}
	}
	return NewService(reg, season.NewCalendar(), ledger), reg, ledger
}

func TestMachineNextAndAllowed(t *testing.T) {
	m := NewMachine()
	if got, err := m.Next(model.StateDraft, ActionSubmit); err != nil || got != model.StateSubmitted {
		t.Fatalf("draft+submit = %s, %v", got, err)
	}
	if _, err := m.Next(model.StateDraft, ActionApprove); !errors.Is(err, model.ErrStateConflict) {
		t.Fatalf("draft+approve 应返回 ErrStateConflict, 实际 %v", err)
	}
	if _, err := m.Next(model.StateClosed, ActionSubmit); !errors.Is(err, model.ErrStateConflict) {
		t.Fatalf("closed+submit 应返回 ErrStateConflict, 实际 %v", err)
	}
	allowed := m.Allowed(model.StateSubmitted)
	if len(allowed) != 2 {
		t.Fatalf("submitted 允许动作 = %v, 期望 2 个", allowed)
	}
}

func TestMachineApplyKeepsHistory(t *testing.T) {
	m := NewMachine()
	at := mustTime(t, "2026-08-16T13:00:00+08:00")
	p := model.Permit{ID: "P-0001", State: model.StateDraft}
	p, err := m.Apply(p, ActionSubmit, "港长", "首次提交", at)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	p, err = m.Apply(p, ActionReview, "渔政", "材料齐全", at.Add(time.Hour))
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if len(p.History) != 2 {
		t.Fatalf("流转记录 = %d 条, 期望 2 条", len(p.History))
	}
	last, ok := p.LastTransition()
	if !ok || last.To != model.StateReviewed || last.Operator != "渔政" {
		t.Fatalf("最后一次流转 = %+v", last)
	}
}

// TestApproveDuringClosureExposesSeasonError 断言休渔期内核准出港许可时，
// 返回的错误可以通过 errors.Is 识别为 model.ErrSeasonClosed。
func TestApproveDuringClosureExposesSeasonError(t *testing.T) {
	svc, _, _ := newFixture(t)
	departAt := mustTime(t, "2026-06-20T05:00:00+08:00")
	p, err := svc.Create(Request{VesselID: "YJ-001", DepartAt: departAt, ReturnBy: departAt.Add(72 * time.Hour), Operator: "港长"})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	ctx := context.Background()
	if _, err := svc.Advance(ctx, p.ID, ActionSubmit, "港长", "", departAt); err != nil {
		t.Fatalf("submit 失败: %v", err)
	}
	if _, err := svc.Advance(ctx, p.ID, ActionReview, "渔政", "", departAt); err != nil {
		t.Fatalf("review 失败: %v", err)
	}
	_, err = svc.Advance(ctx, p.ID, ActionApprove, "渔政", "", departAt)
	if err == nil {
		t.Fatalf("休渔期内核准应失败")
	}
	if !errors.Is(err, model.ErrSeasonClosed) {
		t.Fatalf("errors.Is(err, model.ErrSeasonClosed) = false, 错误为 %v", err)
	}
}

// TestAdvanceUnknownPermitExposesSentinel 断言许可不存在时错误链保留 model.ErrPermitUnknown。
func TestAdvanceUnknownPermitExposesSentinel(t *testing.T) {
	svc, _, _ := newFixture(t)
	_, err := svc.Advance(context.Background(), "P-9999", ActionSubmit, "港长", "", time.Now())
	if !errors.Is(err, model.ErrPermitUnknown) {
		t.Fatalf("errors.Is(err, model.ErrPermitUnknown) = false, 错误为 %v", err)
	}
}

// TestAdvanceStateConflictExposesSentinel 断言状态流转冲突时错误链保留 model.ErrStateConflict。
func TestAdvanceStateConflictExposesSentinel(t *testing.T) {
	svc, _, _ := newFixture(t)
	departAt := mustTime(t, "2026-08-18T05:00:00+08:00")
	p, err := svc.Create(Request{VesselID: "YJ-001", DepartAt: departAt, Operator: "港长"})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	_, err = svc.Advance(context.Background(), p.ID, ActionReview, "渔政", "", departAt)
	if !errors.Is(err, model.ErrStateConflict) {
		t.Fatalf("errors.Is(err, model.ErrStateConflict) = false, 错误为 %v", err)
	}
}

// TestApproveWithoutQuotaExposesSentinel 断言渔船无配额时错误链保留 model.ErrQuotaExceeded。
func TestApproveWithoutQuotaExposesSentinel(t *testing.T) {
	reg := registry.New()
	if err := reg.AddPort(model.Port{Code: "zhapo", Name: "闸坡渔港", Province: "广东", Zone: model.ZoneNanhaiNorth, Berths: 60}); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	if err := reg.AddVessel(model.Vessel{ID: "YJ-777", Name: "粤阳江渔00777", HomePort: "zhapo", Class: model.ClassGillnet, PowerKW: 90, CrewSize: 6, Registered: mustTime(t, "2021-01-01T00:00:00Z")}); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	svc := NewService(reg, season.NewCalendar(), quota.NewLedger())
	departAt := mustTime(t, "2026-08-18T05:00:00+08:00")
	p, err := svc.Create(Request{VesselID: "YJ-777", DepartAt: departAt, Operator: "港长"})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	ctx := context.Background()
	if _, err := svc.Advance(ctx, p.ID, ActionSubmit, "港长", "", departAt); err != nil {
		t.Fatalf("submit 失败: %v", err)
	}
	if _, err := svc.Advance(ctx, p.ID, ActionReview, "渔政", "", departAt); err != nil {
		t.Fatalf("review 失败: %v", err)
	}
	_, err = svc.Advance(ctx, p.ID, ActionApprove, "渔政", "", departAt)
	if !errors.Is(err, model.ErrQuotaExceeded) {
		t.Fatalf("errors.Is(err, model.ErrQuotaExceeded) = false, 错误为 %v", err)
	}
}

// TestCreateUnknownVesselExposesSentinel 断言渔船未登记时错误链保留 model.ErrVesselUnknown。
func TestCreateUnknownVesselExposesSentinel(t *testing.T) {
	svc, _, _ := newFixture(t)
	_, err := svc.Create(Request{VesselID: "NOPE-1", DepartAt: mustTime(t, "2026-08-18T05:00:00+08:00")})
	if !errors.Is(err, model.ErrVesselUnknown) {
		t.Fatalf("errors.Is(err, model.ErrVesselUnknown) = false, 错误为 %v", err)
	}
}

// TestApproveAtReopeningInstant 断言开渔时刻的出港许可可以核准通过。
func TestApproveAtReopeningInstant(t *testing.T) {
	svc, _, _ := newFixture(t)
	departAt := mustTime(t, "2026-08-16T12:00:00+08:00")
	p, err := svc.Create(Request{VesselID: "YJ-001", DepartAt: departAt, ReturnBy: departAt.Add(96 * time.Hour), Operator: "港长"})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	ctx := context.Background()
	for _, action := range []Action{ActionSubmit, ActionReview, ActionApprove} {
		if _, err := svc.Advance(ctx, p.ID, action, "渔政", "", departAt); err != nil {
			t.Fatalf("%s 失败: %v", action, err)
		}
	}
	got, err := svc.Get(p.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.State != model.StateApproved {
		t.Fatalf("开渔时刻出港许可状态 = %s, 期望 %s", got.State, model.StateApproved)
	}
}

func TestApproveExemptClassDuringClosure(t *testing.T) {
	svc, _, _ := newFixture(t)
	departAt := mustTime(t, "2026-06-20T05:00:00+08:00")
	p, err := svc.Create(Request{VesselID: "YJ-009", DepartAt: departAt, Operator: "港长"})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	ctx := context.Background()
	for _, action := range []Action{ActionSubmit, ActionReview, ActionApprove} {
		if _, err := svc.Advance(ctx, p.ID, action, "渔政", "", departAt); err != nil {
			t.Fatalf("%s 失败: %v", action, err)
		}
	}
	got, err := svc.Get(p.ID)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.State != model.StateApproved {
		t.Fatalf("单船钓休渔期内许可状态 = %s, 期望 %s", got.State, model.StateApproved)
	}
}

func TestAdvanceHonoursCancelledContext(t *testing.T) {
	svc, _, _ := newFixture(t)
	departAt := mustTime(t, "2026-08-18T05:00:00+08:00")
	p, err := svc.Create(Request{VesselID: "YJ-001", DepartAt: departAt, Operator: "港长"})
	if err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Advance(ctx, p.ID, ActionSubmit, "港长", "", departAt); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消的 context 应返回 context.Canceled, 实际 %v", err)
	}
}

func TestListAndCounts(t *testing.T) {
	svc, _, _ := newFixture(t)
	departAt := mustTime(t, "2026-08-18T05:00:00+08:00")
	for i := 0; i < 3; i++ {
		if _, err := svc.Create(Request{VesselID: "YJ-001", DepartAt: departAt.Add(time.Duration(i) * time.Hour), Operator: "港长"}); err != nil {
			t.Fatalf("Create 失败: %v", err)
		}
	}
	if got := len(svc.List()); got != 3 {
		t.Fatalf("许可数 = %d, 期望 3", got)
	}
	if got := svc.Counts()[model.StateDraft]; got != 3 {
		t.Fatalf("draft 数 = %d, 期望 3", got)
	}
	if got := len(svc.ListByState(model.StateApproved)); got != 0 {
		t.Fatalf("approved 数 = %d, 期望 0", got)
	}
}
