// Package registry 维护渔船与渔港的登记信息。
package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"nanhaiport/internal/model"
)

// Registry 是渔船与渔港的线程安全登记簿。
type Registry struct {
	mu      sync.RWMutex
	vessels map[string]model.Vessel
	ports   map[string]model.Port
}

// New 构造空登记簿。
func New() *Registry {
	return &Registry{
		vessels: make(map[string]model.Vessel),
		ports:   make(map[string]model.Port),
	}
}

// AddPort 登记渔港。
func (r *Registry) AddPort(p model.Port) error {
	if strings.TrimSpace(p.Code) == "" {
		return fmt.Errorf("registry: 渔港代码为空")
	}
	if p.Berths <= 0 {
		return fmt.Errorf("registry: 渔港 %s 泊位数必须为正", p.Code)
	}
	if _, err := model.ParseZone(string(p.Zone)); err != nil {
		return fmt.Errorf("registry: 渔港 %s 海区非法: %w", p.Code, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ports[p.Code] = p
	return nil
}

// Port 返回渔港信息。
func (r *Registry) Port(code string) (model.Port, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.ports[code]
	if !ok {
		return model.Port{}, fmt.Errorf("%w: %s", model.ErrPortUnknown, code)
	}
	return p, nil
}

// Ports 返回全部渔港，按代码排序。
func (r *Registry) Ports() []model.Port {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Port, 0, len(r.ports))
	for _, p := range r.ports {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// AddVessel 登记渔船。重复编号返回 model.ErrDuplicateVessel。
func (r *Registry) AddVessel(v model.Vessel) error {
	if err := v.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.vessels[v.ID]; exists {
		return fmt.Errorf("%w: %s", model.ErrDuplicateVessel, v.ID)
	}
	if _, ok := r.ports[v.HomePort]; !ok {
		return fmt.Errorf("%w: 渔船 %s 的船籍港 %s", model.ErrPortUnknown, v.ID, v.HomePort)
	}
	r.vessels[v.ID] = v
	return nil
}

// Vessel 返回渔船信息。
func (r *Registry) Vessel(id string) (model.Vessel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.vessels[id]
	if !ok {
		return model.Vessel{}, fmt.Errorf("%w: %s", model.ErrVesselUnknown, id)
	}
	return v, nil
}

// ActiveVessel 返回未注销的渔船信息。
func (r *Registry) ActiveVessel(id string) (model.Vessel, error) {
	v, err := r.Vessel(id)
	if err != nil {
		return model.Vessel{}, err
	}
	if v.Retired {
		return model.Vessel{}, fmt.Errorf("%w: %s", model.ErrVesselRetired, id)
	}
	return v, nil
}

// Retire 注销渔船。
func (r *Registry) Retire(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.vessels[id]
	if !ok {
		return fmt.Errorf("%w: %s", model.ErrVesselUnknown, id)
	}
	v.Retired = true
	r.vessels[id] = v
	return nil
}

// Vessels 返回全部渔船，按编号排序。
func (r *Registry) Vessels() []model.Vessel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]model.Vessel, 0, len(r.vessels))
	for _, v := range r.vessels {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// VesselsByPort 返回指定渔港的渔船，按编号排序。
func (r *Registry) VesselsByPort(code string) []model.Vessel {
	all := r.Vessels()
	out := make([]model.Vessel, 0, len(all))
	for _, v := range all {
		if v.HomePort == code {
			out = append(out, v)
		}
	}
	return out
}

// ZoneOf 返回渔船所属海区。
func (r *Registry) ZoneOf(vesselID string) (model.Zone, error) {
	v, err := r.Vessel(vesselID)
	if err != nil {
		return "", err
	}
	p, err := r.Port(v.HomePort)
	if err != nil {
		return "", err
	}
	return p.Zone, nil
}

// Snapshot 是登记簿的可序列化快照。
type Snapshot struct {
	Ports   []model.Port   `json:"ports"`
	Vessels []model.Vessel `json:"vessels"`
}

// Export 导出登记簿快照。
func (r *Registry) Export() Snapshot {
	return Snapshot{Ports: r.Ports(), Vessels: r.Vessels()}
}

// WriteJSON 将登记簿写为缩进 JSON。
func (r *Registry) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r.Export()); err != nil {
		return fmt.Errorf("registry: 导出登记簿失败: %w", err)
	}
	return nil
}

// LoadJSON 从 JSON 读入登记簿内容，覆盖已有数据。
func LoadJSON(rd io.Reader) (*Registry, error) {
	var snap Snapshot
	if err := json.NewDecoder(rd).Decode(&snap); err != nil {
		return nil, fmt.Errorf("registry: 读取登记簿失败: %w", err)
	}
	reg := New()
	for _, p := range snap.Ports {
		if err := reg.AddPort(p); err != nil {
			return nil, err
		}
	}
	for _, v := range snap.Vessels {
		retired := v.Retired
		v.Retired = false
		if err := reg.AddVessel(v); err != nil {
			return nil, err
		}
		if retired {
			if err := reg.Retire(v.ID); err != nil {
				return nil, err
			}
		}
	}
	return reg, nil
}

// Counts 汇总登记簿规模。
type Counts struct {
	Ports    int `json:"ports"`
	Vessels  int `json:"vessels"`
	Retired  int `json:"retired"`
	Exempted int `json:"exempted"`
}

// Counts 返回登记簿规模统计。
func (r *Registry) Counts() Counts {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := Counts{Ports: len(r.ports), Vessels: len(r.vessels)}
	for _, v := range r.vessels {
		if v.Retired {
			c.Retired++
		}
		if v.Class.ExemptFromClosure() {
			c.Exempted++
		}
	}
	return c
}
