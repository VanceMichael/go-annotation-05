// Package model 定义南海伏季休渔渔港调度平台的领域模型。
//
// 平台服务对象是南海海域（北纬 12 度以北）沿岸渔港的休渔期管理，
// 覆盖渔船登记、出港许可、捕捞配额台账、靠泊调度与统计报表。
package model

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Zone 表示休渔管理海区。
type Zone string

const (
	// ZoneNanhaiNorth 南海北部（北纬 12 度以北，不含北部湾与琼州海峡）。
	ZoneNanhaiNorth Zone = "nanhai-north"
	// ZoneBeibuGulf 北部湾海区。
	ZoneBeibuGulf Zone = "beibu-gulf"
	// ZoneQiongzhou 琼州海峡海区。
	ZoneQiongzhou Zone = "qiongzhou"
	// ZoneNanhaiSouth 南海南部（北纬 12 度以南），不实行伏季休渔。
	ZoneNanhaiSouth Zone = "nanhai-south"
)

// AllZones 返回平台支持的全部海区，顺序稳定。
func AllZones() []Zone {
	return []Zone{ZoneNanhaiNorth, ZoneBeibuGulf, ZoneQiongzhou, ZoneNanhaiSouth}
}

// ParseZone 解析海区代码。
func ParseZone(s string) (Zone, error) {
	z := Zone(strings.ToLower(strings.TrimSpace(s)))
	for _, known := range AllZones() {
		if z == known {
			return z, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownZone, s)
}

// DisplayName 返回海区中文名。
func (z Zone) DisplayName() string {
	switch z {
	case ZoneNanhaiNorth:
		return "南海北部"
	case ZoneBeibuGulf:
		return "北部湾"
	case ZoneQiongzhou:
		return "琼州海峡"
	case ZoneNanhaiSouth:
		return "南海南部"
	default:
		return string(z)
	}
}

// VesselClass 表示作业类型。
type VesselClass string

const (
	// ClassTrawler 拖网。
	ClassTrawler VesselClass = "trawler"
	// ClassPurseSeine 围网。
	ClassPurseSeine VesselClass = "purse-seine"
	// ClassGillnet 刺网。
	ClassGillnet VesselClass = "gillnet"
	// ClassLightPurse 灯光围网。
	ClassLightPurse VesselClass = "light-purse"
	// ClassSingleHook 单船钓，休渔期内允许作业。
	ClassSingleHook VesselClass = "single-hook"
	// ClassFixedNet 定置张网，休渔期内允许作业。
	ClassFixedNet VesselClass = "fixed-net"
)

// AllClasses 返回全部作业类型。
func AllClasses() []VesselClass {
	return []VesselClass{
		ClassTrawler, ClassPurseSeine, ClassGillnet,
		ClassLightPurse, ClassSingleHook, ClassFixedNet,
	}
}

// ParseVesselClass 解析作业类型代码。
func ParseVesselClass(s string) (VesselClass, error) {
	c := VesselClass(strings.ToLower(strings.TrimSpace(s)))
	for _, known := range AllClasses() {
		if c == known {
			return c, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownVesselClass, s)
}

// DisplayName 返回作业类型中文名。
func (c VesselClass) DisplayName() string {
	switch c {
	case ClassTrawler:
		return "拖网"
	case ClassPurseSeine:
		return "围网"
	case ClassGillnet:
		return "刺网"
	case ClassLightPurse:
		return "灯光围网"
	case ClassSingleHook:
		return "单船钓"
	case ClassFixedNet:
		return "定置张网"
	default:
		return string(c)
	}
}

// ExemptFromClosure 报告该作业类型是否不受伏季休渔限制。
func (c VesselClass) ExemptFromClosure() bool {
	return c == ClassSingleHook || c == ClassFixedNet
}

// Port 表示一个渔港。
type Port struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Province string `json:"province"`
	Zone     Zone   `json:"zone"`
	Berths   int    `json:"berths"`
}

// Vessel 表示一条登记渔船。
type Vessel struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	HomePort   string      `json:"home_port"`
	Class      VesselClass `json:"class"`
	PowerKW    float64     `json:"power_kw"`
	CrewSize   int         `json:"crew_size"`
	Registered time.Time   `json:"registered"`
	Retired    bool        `json:"retired"`
}

// Validate 校验渔船登记信息的完整性。
func (v Vessel) Validate() error {
	if strings.TrimSpace(v.ID) == "" {
		return fmt.Errorf("%w: 渔船编号为空", ErrInvalidVessel)
	}
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("%w: 渔船 %s 船名为空", ErrInvalidVessel, v.ID)
	}
	if strings.TrimSpace(v.HomePort) == "" {
		return fmt.Errorf("%w: 渔船 %s 未填写船籍港", ErrInvalidVessel, v.ID)
	}
	if _, err := ParseVesselClass(string(v.Class)); err != nil {
		return fmt.Errorf("%w: 渔船 %s 作业类型非法", ErrInvalidVessel, v.ID)
	}
	if v.PowerKW <= 0 {
		return fmt.Errorf("%w: 渔船 %s 主机功率必须为正", ErrInvalidVessel, v.ID)
	}
	if v.CrewSize <= 0 {
		return fmt.Errorf("%w: 渔船 %s 船员数必须为正", ErrInvalidVessel, v.ID)
	}
	if v.Registered.IsZero() {
		return fmt.Errorf("%w: 渔船 %s 缺少登记日期", ErrInvalidVessel, v.ID)
	}
	return nil
}

// PowerTier 按主机功率划分吨位档，用于配额分配。
func (v Vessel) PowerTier() string {
	switch {
	case v.PowerKW < 44:
		return "small"
	case v.PowerKW < 250:
		return "medium"
	default:
		return "large"
	}
}

// PermitState 表示出港许可状态。
type PermitState string

const (
	// StateDraft 起草。
	StateDraft PermitState = "draft"
	// StateSubmitted 已提交待审。
	StateSubmitted PermitState = "submitted"
	// StateReviewed 已初审。
	StateReviewed PermitState = "reviewed"
	// StateApproved 已核准，可出港。
	StateApproved PermitState = "approved"
	// StateRejected 已驳回。
	StateRejected PermitState = "rejected"
	// StateSuspended 已暂停。
	StateSuspended PermitState = "suspended"
	// StateClosed 已归档。
	StateClosed PermitState = "closed"
)

// DisplayName 返回许可状态中文名。
func (s PermitState) DisplayName() string {
	switch s {
	case StateDraft:
		return "起草"
	case StateSubmitted:
		return "待审"
	case StateReviewed:
		return "初审通过"
	case StateApproved:
		return "已核准"
	case StateRejected:
		return "已驳回"
	case StateSuspended:
		return "已暂停"
	case StateClosed:
		return "已归档"
	default:
		return string(s)
	}
}

// Terminal 报告该状态是否为终态。
func (s PermitState) Terminal() bool {
	return s == StateRejected || s == StateClosed
}

// Transition 记录一次状态流转。
type Transition struct {
	From     PermitState `json:"from"`
	To       PermitState `json:"to"`
	At       time.Time   `json:"at"`
	Operator string      `json:"operator"`
	Note     string      `json:"note"`
}

// Permit 表示一份出港许可。
type Permit struct {
	ID        string       `json:"id"`
	VesselID  string       `json:"vessel_id"`
	PortCode  string       `json:"port_code"`
	Zone      Zone         `json:"zone"`
	State     PermitState  `json:"state"`
	DepartAt  time.Time    `json:"depart_at"`
	ReturnBy  time.Time    `json:"return_by"`
	History   []Transition `json:"history"`
	CreatedAt time.Time    `json:"created_at"`
}

// Duration 返回计划航次时长。
func (p Permit) Duration() time.Duration {
	if p.DepartAt.IsZero() || p.ReturnBy.IsZero() {
		return 0
	}
	return p.ReturnBy.Sub(p.DepartAt)
}

// LastTransition 返回最近一次流转记录。
func (p Permit) LastTransition() (Transition, bool) {
	if len(p.History) == 0 {
		return Transition{}, false
	}
	return p.History[len(p.History)-1], true
}

// CatchRecord 表示一次上岸卸鱼记录。
type CatchRecord struct {
	VesselID string    `json:"vessel_id"`
	PortCode string    `json:"port_code"`
	Species  string    `json:"species"`
	Tonnage  float64   `json:"tonnage"`
	LandedAt time.Time `json:"landed_at"`
}

// Validate 校验卸鱼记录。
func (c CatchRecord) Validate() error {
	if strings.TrimSpace(c.VesselID) == "" {
		return fmt.Errorf("%w: 缺少渔船编号", ErrInvalidCatch)
	}
	if strings.TrimSpace(c.Species) == "" {
		return fmt.Errorf("%w: 渔船 %s 缺少鱼种", ErrInvalidCatch, c.VesselID)
	}
	if c.Tonnage <= 0 {
		return fmt.Errorf("%w: 渔船 %s 卸鱼量必须为正", ErrInvalidCatch, c.VesselID)
	}
	if c.LandedAt.IsZero() {
		return fmt.Errorf("%w: 渔船 %s 缺少上岸时间", ErrInvalidCatch, c.VesselID)
	}
	return nil
}

// SortCatchRecords 按上岸时间、渔船、鱼种排序，保证输出稳定。
func SortCatchRecords(records []CatchRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if !a.LandedAt.Equal(b.LandedAt) {
			return a.LandedAt.Before(b.LandedAt)
		}
		if a.VesselID != b.VesselID {
			return a.VesselID < b.VesselID
		}
		return a.Species < b.Species
	})
}
