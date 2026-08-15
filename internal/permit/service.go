package permit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"nanhaiport/internal/model"
	"nanhaiport/internal/quota"
	"nanhaiport/internal/registry"
	"nanhaiport/internal/season"
)

// Request 是创建出港许可的请求。
type Request struct {
	VesselID string
	DepartAt time.Time
	ReturnBy time.Time
	Operator string
}

// Service 提供出港许可的创建与审批能力。
type Service struct {
	mu       sync.Mutex
	machine  *Machine
	registry *registry.Registry
	calendar *season.Calendar
	ledger   *quota.Ledger
	permits  map[string]model.Permit
	seq      int
}

// NewService 构造许可服务。
func NewService(reg *registry.Registry, cal *season.Calendar, ledger *quota.Ledger) *Service {
	return &Service{
		machine:  NewMachine(),
		registry: reg,
		calendar: cal,
		ledger:   ledger,
		permits:  make(map[string]model.Permit),
	}
}

// Create 创建一份处于起草状态的出港许可。
func (s *Service) Create(req Request) (model.Permit, error) {
	if strings.TrimSpace(req.VesselID) == "" {
		return model.Permit{}, fmt.Errorf("permit: 缺少渔船编号")
	}
	if req.DepartAt.IsZero() {
		return model.Permit{}, fmt.Errorf("permit: 渔船 %s 缺少计划出港时间", req.VesselID)
	}
	if !req.ReturnBy.IsZero() && !req.ReturnBy.After(req.DepartAt) {
		return model.Permit{}, fmt.Errorf("permit: 渔船 %s 的回港时间必须晚于出港时间", req.VesselID)
	}

	vessel, err := s.registry.ActiveVessel(req.VesselID)
	if err != nil {
		return model.Permit{}, decorate(string(ActionSubmit), req.VesselID, err)
	}
	zone, err := s.registry.ZoneOf(vessel.ID)
	if err != nil {
		return model.Permit{}, decorate(string(ActionSubmit), req.VesselID, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := fmt.Sprintf("P-%04d", s.seq)
	p := model.Permit{
		ID:        id,
		VesselID:  vessel.ID,
		PortCode:  vessel.HomePort,
		Zone:      zone,
		State:     model.StateDraft,
		DepartAt:  req.DepartAt,
		ReturnBy:  req.ReturnBy,
		CreatedAt: req.DepartAt,
	}
	s.permits[id] = p
	return p, nil
}

// Get 返回一份出港许可。
func (s *Service) Get(id string) (model.Permit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.permits[id]
	if !ok {
		return model.Permit{}, fmt.Errorf("%w: %s", model.ErrPermitUnknown, id)
	}
	return p, nil
}

// List 返回全部出港许可，按编号排序。
func (s *Service) List() []model.Permit {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Permit, 0, len(s.permits))
	for _, p := range s.permits {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ListByState 返回指定状态的出港许可。
func (s *Service) ListByState(state model.PermitState) []model.Permit {
	all := s.List()
	out := make([]model.Permit, 0, len(all))
	for _, p := range all {
		if p.State == state {
			out = append(out, p)
		}
	}
	return out
}

// Advance 对一份许可执行审批动作。核准动作会附加休渔期与配额校验。
func (s *Service) Advance(ctx context.Context, id string, action Action, operator, note string, at time.Time) (model.Permit, error) {
	if err := ctx.Err(); err != nil {
		return model.Permit{}, fmt.Errorf("permit: 审批 %s 被中止: %w", id, err)
	}

	current, err := s.Get(id)
	if err != nil {
		return model.Permit{}, decorate(string(action), id, err)
	}

	if action == ActionApprove {
		if err := s.checkDeparture(current); err != nil {
			return model.Permit{}, decorate(string(action), id, err)
		}
	}

	next, err := s.machine.Apply(current, action, operator, note, at)
	if err != nil {
		return model.Permit{}, decorate(string(action), id, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.permits[id] = next
	return next, nil
}

// checkDeparture 校准出港条件：渔船有效、海区已开渔、渔港有泊位、配额未用尽。
func (s *Service) checkDeparture(p model.Permit) error {
	vessel, err := s.registry.ActiveVessel(p.VesselID)
	if err != nil {
		return err
	}
	mayDepart, err := s.calendar.MayDepart(p.DepartAt, p.Zone, vessel.Class)
	if err != nil {
		return err
	}
	if !mayDepart {
		st, serr := s.calendar.StatusAt(p.DepartAt, p.Zone)
		if serr != nil {
			return serr
		}
		return fmt.Errorf("%w: %s 距开渔还有 %d 天", model.ErrSeasonClosed, p.Zone.DisplayName(), st.RemainingDays)
	}
	port, err := s.registry.Port(p.PortCode)
	if err != nil {
		return err
	}
	if port.Berths <= 0 {
		return fmt.Errorf("%w: %s", model.ErrNoBerth, port.Name)
	}
	if s.ledger != nil && s.ledger.TotalQuotaFor(p.VesselID) <= 0 {
		return fmt.Errorf("%w: 渔船 %s 未获得任何配额", model.ErrQuotaExceeded, p.VesselID)
	}
	return nil
}

// Counts 返回按状态分组的许可数量。
func (s *Service) Counts() map[model.PermitState]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[model.PermitState]int)
	for _, p := range s.permits {
		out[p.State]++
	}
	return out
}

// decorate 为审批错误补充动作与许可上下文，同时保留原始错误链，
// 便于上层通过 errors.Is 判定错误类别并映射到对应的状态码与退出码。
func decorate(action, id string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("permit: %s %s 失败: %w", action, id, err)
}
