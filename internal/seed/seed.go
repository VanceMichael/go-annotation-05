// Package seed 提供内置的渔港、渔船、配额与卸鱼样例数据。
//
// 样例覆盖广东、广西、海南三省区的代表性渔港，用于本地演练与自检。
package seed

import (
	"fmt"
	"time"

	"nanhaiport/internal/model"
	"nanhaiport/internal/quota"
	"nanhaiport/internal/registry"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Ports 返回内置渔港清单。
func Ports() []model.Port {
	return []model.Port{
		{Code: "zhapo", Name: "闸坡渔港", Province: "广东", Zone: model.ZoneNanhaiNorth, Berths: 120},
		{Code: "shenjiamen-nh", Name: "硇洲渔港", Province: "广东", Zone: model.ZoneNanhaiNorth, Berths: 86},
		{Code: "beihai", Name: "北海电建渔港", Province: "广西", Zone: model.ZoneBeibuGulf, Berths: 94},
		{Code: "qinzhou", Name: "钦州犀牛脚渔港", Province: "广西", Zone: model.ZoneBeibuGulf, Berths: 52},
		{Code: "haikou", Name: "海口新港", Province: "海南", Zone: model.ZoneQiongzhou, Berths: 68},
		{Code: "sanya", Name: "三亚崖州中心渔港", Province: "海南", Zone: model.ZoneNanhaiSouth, Berths: 140},
	}
}

// Vessels 返回内置渔船清单。
func Vessels() []model.Vessel {
	return []model.Vessel{
		{ID: "YJ-001", Name: "粤阳江渔00121", HomePort: "zhapo", Class: model.ClassTrawler, PowerKW: 316, CrewSize: 14, Registered: day(2016, time.March, 8)},
		{ID: "YJ-002", Name: "粤阳江渔00238", HomePort: "zhapo", Class: model.ClassPurseSeine, PowerKW: 268, CrewSize: 12, Registered: day(2017, time.July, 19)},
		{ID: "YJ-003", Name: "粤阳江渔00347", HomePort: "zhapo", Class: model.ClassLightPurse, PowerKW: 402, CrewSize: 18, Registered: day(2019, time.January, 25)},
		{ID: "YJ-004", Name: "粤阳江渔00456", HomePort: "zhapo", Class: model.ClassSingleHook, PowerKW: 58, CrewSize: 5, Registered: day(2020, time.May, 6)},
		{ID: "ZZ-001", Name: "粤湛江渔01120", HomePort: "shenjiamen-nh", Class: model.ClassTrawler, PowerKW: 224, CrewSize: 11, Registered: day(2015, time.November, 2)},
		{ID: "ZZ-002", Name: "粤湛江渔01233", HomePort: "shenjiamen-nh", Class: model.ClassGillnet, PowerKW: 96, CrewSize: 7, Registered: day(2018, time.September, 14)},
		{ID: "ZZ-003", Name: "粤湛江渔01341", HomePort: "shenjiamen-nh", Class: model.ClassFixedNet, PowerKW: 41, CrewSize: 4, Registered: day(2021, time.April, 3)},
		{ID: "BH-001", Name: "桂北海渔02110", HomePort: "beihai", Class: model.ClassTrawler, PowerKW: 188, CrewSize: 10, Registered: day(2014, time.June, 21)},
		{ID: "BH-002", Name: "桂北海渔02219", HomePort: "beihai", Class: model.ClassPurseSeine, PowerKW: 252, CrewSize: 13, Registered: day(2019, time.August, 30)},
		{ID: "QZ-001", Name: "桂钦州渔03108", HomePort: "qinzhou", Class: model.ClassGillnet, PowerKW: 74, CrewSize: 6, Registered: day(2020, time.February, 11)},
		{ID: "HK-001", Name: "琼海口渔04117", HomePort: "haikou", Class: model.ClassTrawler, PowerKW: 205, CrewSize: 9, Registered: day(2013, time.December, 5)},
		{ID: "HK-002", Name: "琼海口渔04226", HomePort: "haikou", Class: model.ClassSingleHook, PowerKW: 62, CrewSize: 5, Registered: day(2022, time.March, 17)},
		{ID: "SY-001", Name: "琼三亚渔05135", HomePort: "sanya", Class: model.ClassLightPurse, PowerKW: 486, CrewSize: 22, Registered: day(2018, time.October, 9)},
		{ID: "SY-002", Name: "琼三亚渔05244", HomePort: "sanya", Class: model.ClassTrawler, PowerKW: 358, CrewSize: 16, Registered: day(2021, time.July, 28)},
	}
}

// Allocations 返回内置配额清单。
func Allocations() []quota.Allocation {
	return []quota.Allocation{
		{VesselID: "YJ-001", Species: "带鱼", Tonnes: 132},
		{VesselID: "YJ-001", Species: "金线鱼", Tonnes: 76},
		{VesselID: "YJ-002", Species: "蓝圆鲹", Tonnes: 148},
		{VesselID: "YJ-002", Species: "带鱼", Tonnes: 64},
		{VesselID: "YJ-003", Species: "蓝圆鲹", Tonnes: 210},
		{VesselID: "YJ-004", Species: "石斑鱼", Tonnes: 18},
		{VesselID: "ZZ-001", Species: "带鱼", Tonnes: 104},
		{VesselID: "ZZ-002", Species: "二长棘鲷", Tonnes: 58},
		{VesselID: "ZZ-003", Species: "虾类", Tonnes: 26},
		{VesselID: "BH-001", Species: "带鱼", Tonnes: 88},
		{VesselID: "BH-001", Species: "二长棘鲷", Tonnes: 42},
		{VesselID: "BH-002", Species: "蓝圆鲹", Tonnes: 126},
		{VesselID: "QZ-001", Species: "虾类", Tonnes: 34},
		{VesselID: "HK-001", Species: "带鱼", Tonnes: 92},
		{VesselID: "HK-002", Species: "石斑鱼", Tonnes: 21},
		{VesselID: "SY-001", Species: "鲔类", Tonnes: 268},
		{VesselID: "SY-002", Species: "蓝圆鲹", Tonnes: 174},
	}
}

// Landings 返回内置卸鱼记录。
func Landings() []model.CatchRecord {
	base := time.Date(2026, time.August, 17, 6, 0, 0, 0, time.UTC)
	return []model.CatchRecord{
		{VesselID: "YJ-001", PortCode: "zhapo", Species: "带鱼", Tonnage: 41.5, LandedAt: base},
		{VesselID: "YJ-001", PortCode: "zhapo", Species: "金线鱼", Tonnage: 12.0, LandedAt: base.Add(4 * time.Hour)},
		{VesselID: "YJ-002", PortCode: "zhapo", Species: "蓝圆鲹", Tonnage: 63.25, LandedAt: base.Add(9 * time.Hour)},
		{VesselID: "ZZ-001", PortCode: "shenjiamen-nh", Species: "带鱼", Tonnage: 28.75, LandedAt: base.Add(26 * time.Hour)},
		{VesselID: "BH-001", PortCode: "beihai", Species: "带鱼", Tonnage: 33.0, LandedAt: base.Add(31 * time.Hour)},
		{VesselID: "SY-001", PortCode: "sanya", Species: "鲔类", Tonnage: 88.5, LandedAt: base.Add(52 * time.Hour)},
	}
}

// Load 构造带内置样例数据的登记簿与配额台账。
func Load() (*registry.Registry, *quota.Ledger, error) {
	reg := registry.New()
	for _, p := range Ports() {
		if err := reg.AddPort(p); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记渔港 %s 失败: %w", p.Code, err)
		}
	}
	for _, v := range Vessels() {
		if err := reg.AddVessel(v); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记渔船 %s 失败: %w", v.ID, err)
		}
	}
	ledger := quota.NewLedger()
	for _, a := range Allocations() {
		if err := ledger.Allocate(a); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记配额 %s/%s 失败: %w", a.VesselID, a.Species, err)
		}
	}
	for _, rec := range Landings() {
		if err := ledger.Land(rec); err != nil {
			return nil, nil, fmt.Errorf("seed: 登记卸鱼 %s 失败: %w", rec.VesselID, err)
		}
	}
	return reg, ledger, nil
}

// TotalAllocatedTonnes 返回内置配额总量，供自检使用。
func TotalAllocatedTonnes() float64 {
	var total float64
	for _, a := range Allocations() {
		total += a.Tonnes
	}
	return total
}
