// Package season 实现南海伏季休渔期日历。
//
// 休渔窗口按海区分别规定，起止时刻均为北京时间：
//
//	南海北部  5 月 1 日 12:00 至 8 月 16 日 12:00
//	北部湾    5 月 1 日 12:00 至 8 月 16 日 12:00
//	琼州海峡  5 月 1 日 12:00 至 8 月  1 日 12:00
//	南海南部  不实行伏季休渔
//
// 窗口起点为闭区间，终点为开区间：终点即当年开渔时刻，
// 到达该时刻起渔船即可正常出港。
package season

import (
	"fmt"
	"time"

	"nanhaiport/internal/model"
)

// Beijing 返回平台统一使用的东八区时区。
func Beijing() *time.Location {
	return time.FixedZone("CST", 8*60*60)
}

// Window 表示某年某海区的伏季休渔窗口。
type Window struct {
	Zone  model.Zone `json:"zone"`
	Year  int        `json:"year"`
	Start time.Time  `json:"start"`
	End   time.Time  `json:"end"`
}

// rule 描述一个海区的休渔起止规则。
type rule struct {
	startMonth time.Month
	startDay   int
	startHour  int
	endMonth   time.Month
	endDay     int
	endHour    int
}

var rules = map[model.Zone]rule{
	model.ZoneNanhaiNorth: {time.May, 1, 12, time.August, 16, 12},
	model.ZoneBeibuGulf:   {time.May, 1, 12, time.August, 16, 12},
	model.ZoneQiongzhou:   {time.May, 1, 12, time.August, 1, 12},
}

// WindowFor 返回指定年份与海区的休渔窗口。
// 对不实行伏季休渔的海区返回 model.ErrNoSeasonWindow。
func WindowFor(year int, zone model.Zone) (Window, error) {
	if year < 1979 {
		return Window{}, fmt.Errorf("season: 年份 %d 早于制度实施年份", year)
	}
	r, ok := rules[zone]
	if !ok {
		if _, err := model.ParseZone(string(zone)); err != nil {
			return Window{}, err
		}
		return Window{}, fmt.Errorf("%w: %s", model.ErrNoSeasonWindow, zone.DisplayName())
	}
	loc := Beijing()
	return Window{
		Zone:  zone,
		Year:  year,
		Start: time.Date(year, r.startMonth, r.startDay, r.startHour, 0, 0, 0, loc),
		End:   time.Date(year, r.endMonth, r.endDay, r.endHour, 0, 0, 0, loc),
	}, nil
}

// Contains 报告时刻 t 是否落在休渔窗口内。
// 起点为闭区间，终点为开区间：End 即开渔时刻，本身不属于休渔期。
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.Start) && t.Before(w.End)
}

// Length 返回休渔窗口总时长。
func (w Window) Length() time.Duration {
	return w.End.Sub(w.Start)
}

// Format 返回窗口的可读描述。
func (w Window) Format() string {
	loc := Beijing()
	return fmt.Sprintf("%s %s 至 %s",
		w.Zone.DisplayName(),
		w.Start.In(loc).Format("2006-01-02 15:04"),
		w.End.In(loc).Format("2006-01-02 15:04"),
	)
}

// Phase 表示某时刻相对休渔窗口的阶段。
type Phase string

const (
	// PhaseBeforeClosure 当年休渔期尚未开始。
	PhaseBeforeClosure Phase = "before-closure"
	// PhaseClosed 休渔期内。
	PhaseClosed Phase = "closed"
	// PhaseOpen 已开渔。
	PhaseOpen Phase = "open"
	// PhaseExempt 该海区不实行伏季休渔。
	PhaseExempt Phase = "exempt"
)

// DisplayName 返回阶段中文名。
func (p Phase) DisplayName() string {
	switch p {
	case PhaseBeforeClosure:
		return "休渔前"
	case PhaseClosed:
		return "休渔中"
	case PhaseOpen:
		return "已开渔"
	case PhaseExempt:
		return "不休渔"
	default:
		return string(p)
	}
}

// Status 汇总某时刻的休渔状态。
type Status struct {
	Zone          model.Zone `json:"zone"`
	At            time.Time  `json:"at"`
	Phase         Phase      `json:"phase"`
	Window        *Window    `json:"window,omitempty"`
	RemainingDays int        `json:"remaining_days"`
}

// Closed 报告当前是否处于休渔期。
func (s Status) Closed() bool {
	return s.Phase == PhaseClosed
}

// Calendar 提供按海区查询休渔状态的能力。
type Calendar struct{}

// NewCalendar 构造休渔日历。
func NewCalendar() *Calendar {
	return &Calendar{}
}

// StatusAt 返回指定时刻、指定海区的休渔状态。
func (c *Calendar) StatusAt(at time.Time, zone model.Zone) (Status, error) {
	if _, err := model.ParseZone(string(zone)); err != nil {
		return Status{}, err
	}
	if _, ok := rules[zone]; !ok {
		return Status{Zone: zone, At: at, Phase: PhaseExempt}, nil
	}
	year := at.In(Beijing()).Year()
	w, err := WindowFor(year, zone)
	if err != nil {
		return Status{}, err
	}
	st := Status{Zone: zone, At: at, Window: &w}
	switch {
	case at.Before(w.Start):
		st.Phase = PhaseBeforeClosure
	case w.Contains(at):
		st.Phase = PhaseClosed
		st.RemainingDays = remainingDays(at, w.End)
	default:
		st.Phase = PhaseOpen
	}
	return st, nil
}

// ClosedAt 报告指定时刻该海区是否处于休渔期。
func (c *Calendar) ClosedAt(at time.Time, zone model.Zone) (bool, error) {
	st, err := c.StatusAt(at, zone)
	if err != nil {
		return false, err
	}
	return st.Closed(), nil
}

// MayDepart 报告某作业类型的渔船在指定时刻能否出港。
// 允许出港的条件是该海区已开渔，或该作业类型不受休渔限制。
func (c *Calendar) MayDepart(at time.Time, zone model.Zone, class model.VesselClass) (bool, error) {
	if class.ExemptFromClosure() {
		return true, nil
	}
	closed, err := c.ClosedAt(at, zone)
	if err != nil {
		return false, err
	}
	return !closed, nil
}

// NextOpening 返回指定时刻之后最近的开渔时刻。
func (c *Calendar) NextOpening(at time.Time, zone model.Zone) (time.Time, error) {
	if _, ok := rules[zone]; !ok {
		if _, err := model.ParseZone(string(zone)); err != nil {
			return time.Time{}, err
		}
		return time.Time{}, fmt.Errorf("%w: %s", model.ErrNoSeasonWindow, zone.DisplayName())
	}
	year := at.In(Beijing()).Year()
	for offset := 0; offset < 3; offset++ {
		w, err := WindowFor(year+offset, zone)
		if err != nil {
			return time.Time{}, err
		}
		if w.End.After(at) {
			return w.End, nil
		}
	}
	return time.Time{}, fmt.Errorf("season: 无法确定 %s 的下一次开渔时刻", zone)
}

// remainingDays 返回距开渔时刻还剩的自然天数，向上取整。
func remainingDays(at, end time.Time) int {
	d := end.Sub(at)
	if d <= 0 {
		return 0
	}
	days := int(d / (24 * time.Hour))
	if d%(24*time.Hour) != 0 {
		days++
	}
	return days
}
