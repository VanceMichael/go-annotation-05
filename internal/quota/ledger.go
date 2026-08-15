// Package quota 实现捕捞配额台账。
//
// 台账登记每条渔船按鱼种的年度配额，并累计其上岸卸鱼量，
// 用于开渔后的配额核销、超限预警与渔港汇总。
package quota

import (
	"fmt"
	"sort"
	"strings"

	"nanhaiport/internal/model"
)

// Allocation 表示一条渔船在某鱼种上的年度配额。
type Allocation struct {
	VesselID string  `json:"vessel_id"`
	Species  string  `json:"species"`
	Tonnes   float64 `json:"tonnes"`
}

// Usage 表示某渔船某鱼种的配额使用情况。
type Usage struct {
	VesselID  string  `json:"vessel_id"`
	Species   string  `json:"species"`
	Quota     float64 `json:"quota"`
	Landed    float64 `json:"landed"`
	Remaining float64 `json:"remaining"`
	Exceeded  bool    `json:"exceeded"`
}

// Ledger 是捕捞配额台账。
type Ledger struct {
	allocations []Allocation
	landings    []model.CatchRecord
}

// NewLedger 构造空台账。
func NewLedger() *Ledger {
	return &Ledger{}
}

// Allocate 登记一条配额。同一渔船同一鱼种重复登记时按累加处理。
func (l *Ledger) Allocate(a Allocation) error {
	if strings.TrimSpace(a.VesselID) == "" {
		return fmt.Errorf("quota: 配额缺少渔船编号")
	}
	if strings.TrimSpace(a.Species) == "" {
		return fmt.Errorf("quota: 渔船 %s 的配额缺少鱼种", a.VesselID)
	}
	if a.Tonnes <= 0 {
		return fmt.Errorf("quota: 渔船 %s 的 %s 配额必须为正", a.VesselID, a.Species)
	}
	for i := range l.allocations {
		if l.allocations[i].VesselID == a.VesselID && l.allocations[i].Species == a.Species {
			l.allocations[i].Tonnes += a.Tonnes
			return nil
		}
	}
	l.allocations = append(l.allocations, a)
	return nil
}

// Land 登记一次上岸卸鱼记录。
func (l *Ledger) Land(rec model.CatchRecord) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	l.landings = append(l.landings, rec)
	return nil
}

// Allocations 返回全部配额，按渔船与鱼种排序。
func (l *Ledger) Allocations() []Allocation {
	out := make([]Allocation, len(l.allocations))
	copy(out, l.allocations)
	sortAllocations(out)
	return out
}

// VesselAllocations 返回指定渔船的全部配额，按鱼种排序。
// 返回的切片与台账内部存储互不影响。
func (l *Ledger) VesselAllocations(vesselID string) []Allocation {
	out := make([]Allocation, 0, len(l.allocations))
	for _, a := range l.allocations {
		if a.VesselID == vesselID {
			out = append(out, a)
		}
	}
	sortAllocations(out)
	return out
}

// SpeciesAllocations 返回指定鱼种的全部配额，按渔船排序。
// 返回的切片与台账内部存储互不影响。
func (l *Ledger) SpeciesAllocations(species string) []Allocation {
	out := make([]Allocation, 0, len(l.allocations))
	for _, a := range l.allocations {
		if a.Species == species {
			out = append(out, a)
		}
	}
	sortAllocations(out)
	return out
}

// Landings 返回全部卸鱼记录，按时间排序。
func (l *Ledger) Landings() []model.CatchRecord {
	out := make([]model.CatchRecord, len(l.landings))
	copy(out, l.landings)
	model.SortCatchRecords(out)
	return out
}

// LandedTonnage 返回指定渔船指定鱼种的累计上岸量。
func (l *Ledger) LandedTonnage(vesselID, species string) float64 {
	var total float64
	for _, rec := range l.landings {
		if rec.VesselID == vesselID && rec.Species == species {
			total += rec.Tonnage
		}
	}
	return total
}

// UsageFor 返回指定渔船的配额使用情况，按鱼种排序。
func (l *Ledger) UsageFor(vesselID string) []Usage {
	allocs := l.VesselAllocations(vesselID)
	out := make([]Usage, 0, len(allocs))
	for _, a := range allocs {
		landed := l.LandedTonnage(a.VesselID, a.Species)
		out = append(out, Usage{
			VesselID:  a.VesselID,
			Species:   a.Species,
			Quota:     a.Tonnes,
			Landed:    landed,
			Remaining: a.Tonnes - landed,
			Exceeded:  landed > a.Tonnes,
		})
	}
	return out
}

// CheckLanding 校验一次卸鱼是否会导致配额超限。
func (l *Ledger) CheckLanding(rec model.CatchRecord) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	var quota float64
	found := false
	for _, a := range l.allocations {
		if a.VesselID == rec.VesselID && a.Species == rec.Species {
			quota = a.Tonnes
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: 渔船 %s 未获得 %s 配额", model.ErrQuotaExceeded, rec.VesselID, rec.Species)
	}
	if l.LandedTonnage(rec.VesselID, rec.Species)+rec.Tonnage > quota {
		return fmt.Errorf("%w: 渔船 %s 的 %s 配额 %.2f 吨", model.ErrQuotaExceeded, rec.VesselID, rec.Species, quota)
	}
	return nil
}

// Summary 汇总台账整体情况。
type Summary struct {
	Vessels     int     `json:"vessels"`
	Species     int     `json:"species"`
	Allocations int     `json:"allocations"`
	TotalQuota  float64 `json:"total_quota"`
	TotalLanded float64 `json:"total_landed"`
	Exceeded    int     `json:"exceeded"`
}

// Summary 返回台账汇总。
func (l *Ledger) Summary() Summary {
	s := Summary{Allocations: len(l.allocations)}
	vessels := make(map[string]struct{})
	species := make(map[string]struct{})
	for _, a := range l.allocations {
		vessels[a.VesselID] = struct{}{}
		species[a.Species] = struct{}{}
		s.TotalQuota += a.Tonnes
		if l.LandedTonnage(a.VesselID, a.Species) > a.Tonnes {
			s.Exceeded++
		}
	}
	for _, rec := range l.landings {
		s.TotalLanded += rec.Tonnage
	}
	s.Vessels = len(vessels)
	s.Species = len(species)
	return s
}

// TotalQuotaFor 返回指定渔船的配额总量。
func (l *Ledger) TotalQuotaFor(vesselID string) float64 {
	var total float64
	for _, a := range l.allocations {
		if a.VesselID == vesselID {
			total += a.Tonnes
		}
	}
	return total
}

// VesselIDs 返回台账中出现过的渔船编号，按字典序排序。
func (l *Ledger) VesselIDs() []string {
	seen := make(map[string]struct{}, len(l.allocations))
	for _, a := range l.allocations {
		seen[a.VesselID] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func sortAllocations(items []Allocation) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].VesselID != items[j].VesselID {
			return items[i].VesselID < items[j].VesselID
		}
		return items[i].Species < items[j].Species
	})
}
