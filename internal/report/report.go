// Package report 生成休渔与开渔期的统计报表。
package report

import (
	"fmt"
	"sort"
	"time"

	"nanhaiport/internal/model"
	"nanhaiport/internal/permit"
	"nanhaiport/internal/quota"
	"nanhaiport/internal/registry"
	"nanhaiport/internal/season"
)

// PortLine 是渔港维度的报表行。
type PortLine struct {
	PortCode      string  `json:"port_code"`
	PortName      string  `json:"port_name"`
	Zone          string  `json:"zone"`
	Phase         string  `json:"phase"`
	RemainingDays int     `json:"remaining_days"`
	Vessels       int     `json:"vessels"`
	ExemptVessels int     `json:"exempt_vessels"`
	QuotaTonnes   float64 `json:"quota_tonnes"`
	LandedTonnes  float64 `json:"landed_tonnes"`
	BerthUsage    float64 `json:"berth_usage"`
}

// SeasonReport 是整体休渔报表。
type SeasonReport struct {
	At           time.Time      `json:"at"`
	Ports        []PortLine     `json:"ports"`
	TotalVessels int            `json:"total_vessels"`
	TotalQuota   float64        `json:"total_quota"`
	TotalLanded  float64        `json:"total_landed"`
	QuotaSummary quota.Summary  `json:"quota_summary"`
	PermitCounts map[string]int `json:"permit_counts"`
}

// Builder 组装报表所需的数据源。
type Builder struct {
	registry *registry.Registry
	calendar *season.Calendar
	ledger   *quota.Ledger
	permits  *permit.Service
}

// NewBuilder 构造报表生成器。
func NewBuilder(reg *registry.Registry, cal *season.Calendar, ledger *quota.Ledger, permits *permit.Service) *Builder {
	return &Builder{registry: reg, calendar: cal, ledger: ledger, permits: permits}
}

// Season 生成指定时刻的休渔报表。
func (b *Builder) Season(at time.Time) (SeasonReport, error) {
	rep := SeasonReport{At: at, PermitCounts: map[string]int{}}
	for _, port := range b.registry.Ports() {
		st, err := b.calendar.StatusAt(at, port.Zone)
		if err != nil {
			return SeasonReport{}, fmt.Errorf("report: 渔港 %s 休渔状态查询失败: %w", port.Code, err)
		}
		line := PortLine{
			PortCode:      port.Code,
			PortName:      port.Name,
			Zone:          string(port.Zone),
			Phase:         string(st.Phase),
			RemainingDays: st.RemainingDays,
		}
		vessels := b.registry.VesselsByPort(port.Code)
		for _, v := range vessels {
			if v.Retired {
				continue
			}
			line.Vessels++
			if v.Class.ExemptFromClosure() {
				line.ExemptVessels++
			}
			for _, alloc := range b.ledger.VesselAllocations(v.ID) {
				line.QuotaTonnes += alloc.Tonnes
				line.LandedTonnes += b.ledger.LandedTonnage(v.ID, alloc.Species)
			}
		}
		if port.Berths > 0 {
			line.BerthUsage = float64(line.Vessels) / float64(port.Berths)
		}
		rep.Ports = append(rep.Ports, line)
		rep.TotalVessels += line.Vessels
		rep.TotalQuota += line.QuotaTonnes
		rep.TotalLanded += line.LandedTonnes
	}
	sort.Slice(rep.Ports, func(i, j int) bool { return rep.Ports[i].PortCode < rep.Ports[j].PortCode })
	rep.QuotaSummary = b.ledger.Summary()
	if b.permits != nil {
		for state, count := range b.permits.Counts() {
			rep.PermitCounts[string(state)] = count
		}
	}
	return rep, nil
}

// VesselLine 是渔船维度的报表行。
type VesselLine struct {
	VesselID    string        `json:"vessel_id"`
	VesselName  string        `json:"vessel_name"`
	Class       string        `json:"class"`
	PowerTier   string        `json:"power_tier"`
	Zone        string        `json:"zone"`
	MayDepart   bool          `json:"may_depart"`
	QuotaTonnes float64       `json:"quota_tonnes"`
	Usage       []quota.Usage `json:"usage"`
}

// Vessel 生成单船报表。
func (b *Builder) Vessel(at time.Time, vesselID string) (VesselLine, error) {
	v, err := b.registry.Vessel(vesselID)
	if err != nil {
		return VesselLine{}, err
	}
	zone, err := b.registry.ZoneOf(vesselID)
	if err != nil {
		return VesselLine{}, err
	}
	mayDepart, err := b.calendar.MayDepart(at, zone, v.Class)
	if err != nil {
		return VesselLine{}, err
	}
	line := VesselLine{
		VesselID:   v.ID,
		VesselName: v.Name,
		Class:      string(v.Class),
		PowerTier:  v.PowerTier(),
		Zone:       string(zone),
		MayDepart:  mayDepart,
		Usage:      b.ledger.UsageFor(vesselID),
	}
	line.QuotaTonnes = b.ledger.TotalQuotaFor(vesselID)
	return line, nil
}

// ZoneLine 是海区维度的报表行。
type ZoneLine struct {
	Zone          string  `json:"zone"`
	ZoneName      string  `json:"zone_name"`
	Phase         string  `json:"phase"`
	RemainingDays int     `json:"remaining_days"`
	Window        string  `json:"window,omitempty"`
	Ports         int     `json:"ports"`
	Vessels       int     `json:"vessels"`
	QuotaTonnes   float64 `json:"quota_tonnes"`
}

// Zones 生成海区维度报表。
func (b *Builder) Zones(at time.Time) ([]ZoneLine, error) {
	byZone := make(map[model.Zone]*ZoneLine)
	for _, z := range model.AllZones() {
		st, err := b.calendar.StatusAt(at, z)
		if err != nil {
			return nil, fmt.Errorf("report: 海区 %s 休渔状态查询失败: %w", z, err)
		}
		line := &ZoneLine{
			Zone:          string(z),
			ZoneName:      z.DisplayName(),
			Phase:         string(st.Phase),
			RemainingDays: st.RemainingDays,
		}
		if st.Window != nil {
			line.Window = st.Window.Format()
		}
		byZone[z] = line
	}
	for _, port := range b.registry.Ports() {
		line, ok := byZone[port.Zone]
		if !ok {
			continue
		}
		line.Ports++
		for _, v := range b.registry.VesselsByPort(port.Code) {
			if v.Retired {
				continue
			}
			line.Vessels++
			line.QuotaTonnes += b.ledger.TotalQuotaFor(v.ID)
		}
	}
	out := make([]ZoneLine, 0, len(byZone))
	for _, z := range model.AllZones() {
		out = append(out, *byZone[z])
	}
	return out, nil
}
