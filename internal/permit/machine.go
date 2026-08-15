// Package permit 实现出港许可的状态机与审批服务。
package permit

import (
	"fmt"
	"sort"
	"time"

	"nanhaiport/internal/model"
)

// Action 表示一次审批动作。
type Action string

const (
	// ActionSubmit 提交待审。
	ActionSubmit Action = "submit"
	// ActionReview 初审通过。
	ActionReview Action = "review"
	// ActionApprove 核准。
	ActionApprove Action = "approve"
	// ActionReject 驳回。
	ActionReject Action = "reject"
	// ActionSuspend 暂停。
	ActionSuspend Action = "suspend"
	// ActionResume 恢复。
	ActionResume Action = "resume"
	// ActionClose 归档。
	ActionClose Action = "close"
)

// DisplayName 返回动作中文名。
func (a Action) DisplayName() string {
	switch a {
	case ActionSubmit:
		return "提交"
	case ActionReview:
		return "初审"
	case ActionApprove:
		return "核准"
	case ActionReject:
		return "驳回"
	case ActionSuspend:
		return "暂停"
	case ActionResume:
		return "恢复"
	case ActionClose:
		return "归档"
	default:
		return string(a)
	}
}

// AllActions 返回全部审批动作。
func AllActions() []Action {
	return []Action{
		ActionSubmit, ActionReview, ActionApprove,
		ActionReject, ActionSuspend, ActionResume, ActionClose,
	}
}

// ParseAction 解析审批动作。
func ParseAction(s string) (Action, error) {
	for _, a := range AllActions() {
		if string(a) == s {
			return a, nil
		}
	}
	return "", fmt.Errorf("permit: 未知审批动作 %q", s)
}

// transition 描述一条合法流转边。
type transition struct {
	from   model.PermitState
	action Action
}

var edges = map[transition]model.PermitState{
	{model.StateDraft, ActionSubmit}:     model.StateSubmitted,
	{model.StateDraft, ActionReject}:     model.StateRejected,
	{model.StateSubmitted, ActionReview}: model.StateReviewed,
	{model.StateSubmitted, ActionReject}: model.StateRejected,
	{model.StateReviewed, ActionApprove}: model.StateApproved,
	{model.StateReviewed, ActionReject}:  model.StateRejected,
	{model.StateApproved, ActionSuspend}: model.StateSuspended,
	{model.StateApproved, ActionClose}:   model.StateClosed,
	{model.StateSuspended, ActionResume}: model.StateApproved,
	{model.StateSuspended, ActionClose}:  model.StateClosed,
	{model.StateSuspended, ActionReject}: model.StateRejected,
}

// Machine 是无状态的许可状态机。
type Machine struct{}

// NewMachine 构造状态机。
func NewMachine() *Machine {
	return &Machine{}
}

// Next 返回在当前状态下执行动作后的目标状态。
func (m *Machine) Next(from model.PermitState, action Action) (model.PermitState, error) {
	to, ok := edges[transition{from, action}]
	if !ok {
		return "", fmt.Errorf("%w: 状态 %s 不支持动作 %s",
			model.ErrStateConflict, from.DisplayName(), action.DisplayName())
	}
	return to, nil
}

// Allowed 返回当前状态下允许的动作，顺序稳定。
func (m *Machine) Allowed(from model.PermitState) []Action {
	out := make([]Action, 0, 4)
	for _, a := range AllActions() {
		if _, ok := edges[transition{from, a}]; ok {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Apply 在许可上执行动作并追加流转记录。
func (m *Machine) Apply(p model.Permit, action Action, operator, note string, at time.Time) (model.Permit, error) {
	to, err := m.Next(p.State, action)
	if err != nil {
		return p, err
	}
	next := p
	next.State = to
	history := make([]model.Transition, 0, len(p.History)+1)
	history = append(history, p.History...)
	history = append(history, model.Transition{
		From:     p.State,
		To:       to,
		At:       at,
		Operator: operator,
		Note:     note,
	})
	next.History = history
	return next, nil
}
