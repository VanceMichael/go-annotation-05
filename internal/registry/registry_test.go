package registry

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"nanhaiport/internal/model"
)

func samplePort() model.Port {
	return model.Port{Code: "zhapo", Name: "闸坡渔港", Province: "广东", Zone: model.ZoneNanhaiNorth, Berths: 120}
}

func sampleVessel(id string) model.Vessel {
	return model.Vessel{
		ID:         id,
		Name:       "粤阳江" + id,
		HomePort:   "zhapo",
		Class:      model.ClassTrawler,
		PowerKW:    280,
		CrewSize:   11,
		Registered: time.Date(2019, time.March, 12, 0, 0, 0, 0, time.UTC),
	}
}

func TestAddVesselRequiresKnownPort(t *testing.T) {
	reg := New()
	err := reg.AddVessel(sampleVessel("YJ-001"))
	if !errors.Is(err, model.ErrPortUnknown) {
		t.Fatalf("船籍港未登记时应返回 ErrPortUnknown, 实际 %v", err)
	}
}

func TestAddVesselDuplicate(t *testing.T) {
	reg := New()
	if err := reg.AddPort(samplePort()); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	if err := reg.AddVessel(sampleVessel("YJ-001")); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	if err := reg.AddVessel(sampleVessel("YJ-001")); !errors.Is(err, model.ErrDuplicateVessel) {
		t.Fatalf("重复登记应返回 ErrDuplicateVessel, 实际 %v", err)
	}
}

func TestRetireAndActiveVessel(t *testing.T) {
	reg := New()
	if err := reg.AddPort(samplePort()); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	if err := reg.AddVessel(sampleVessel("YJ-002")); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	if err := reg.Retire("YJ-002"); err != nil {
		t.Fatalf("Retire 失败: %v", err)
	}
	if _, err := reg.ActiveVessel("YJ-002"); !errors.Is(err, model.ErrVesselRetired) {
		t.Fatalf("已注销渔船应返回 ErrVesselRetired, 实际 %v", err)
	}
	if _, err := reg.Vessel("YJ-002"); err != nil {
		t.Fatalf("已注销渔船仍应可查询: %v", err)
	}
}

func TestZoneOf(t *testing.T) {
	reg := New()
	if err := reg.AddPort(samplePort()); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	if err := reg.AddVessel(sampleVessel("YJ-003")); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	zone, err := reg.ZoneOf("YJ-003")
	if err != nil {
		t.Fatalf("ZoneOf 失败: %v", err)
	}
	if zone != model.ZoneNanhaiNorth {
		t.Fatalf("海区 = %s, 期望 %s", zone, model.ZoneNanhaiNorth)
	}
	if _, err := reg.ZoneOf("missing"); !errors.Is(err, model.ErrVesselUnknown) {
		t.Fatalf("未知渔船应返回 ErrVesselUnknown, 实际 %v", err)
	}
}

func TestRoundTripJSON(t *testing.T) {
	reg := New()
	if err := reg.AddPort(samplePort()); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	if err := reg.AddVessel(sampleVessel("YJ-004")); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	hook := sampleVessel("YJ-005")
	hook.Class = model.ClassSingleHook
	if err := reg.AddVessel(hook); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	if err := reg.Retire("YJ-004"); err != nil {
		t.Fatalf("Retire 失败: %v", err)
	}

	var buf bytes.Buffer
	if err := reg.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON 失败: %v", err)
	}
	restored, err := LoadJSON(&buf)
	if err != nil {
		t.Fatalf("LoadJSON 失败: %v", err)
	}
	got := restored.Counts()
	want := Counts{Ports: 1, Vessels: 2, Retired: 1, Exempted: 1}
	if got != want {
		t.Fatalf("Counts = %+v, 期望 %+v", got, want)
	}
}

func TestVesselsByPort(t *testing.T) {
	reg := New()
	if err := reg.AddPort(samplePort()); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	other := model.Port{Code: "beihai", Name: "北海渔港", Province: "广西", Zone: model.ZoneBeibuGulf, Berths: 90}
	if err := reg.AddPort(other); err != nil {
		t.Fatalf("AddPort 失败: %v", err)
	}
	if err := reg.AddVessel(sampleVessel("YJ-010")); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	bh := sampleVessel("BH-001")
	bh.HomePort = "beihai"
	if err := reg.AddVessel(bh); err != nil {
		t.Fatalf("AddVessel 失败: %v", err)
	}
	if got := reg.VesselsByPort("zhapo"); len(got) != 1 || got[0].ID != "YJ-010" {
		t.Fatalf("VesselsByPort(zhapo) = %+v", got)
	}
	if got := reg.VesselsByPort("beihai"); len(got) != 1 || got[0].ID != "BH-001" {
		t.Fatalf("VesselsByPort(beihai) = %+v", got)
	}
}
