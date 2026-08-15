package model

import "errors"

// 领域哨兵错误。上层通过 errors.Is 判定错误类别，禁止依赖错误文本。
var (
	// ErrUnknownZone 海区代码不存在。
	ErrUnknownZone = errors.New("model: 未知海区")
	// ErrUnknownVesselClass 作业类型代码不存在。
	ErrUnknownVesselClass = errors.New("model: 未知作业类型")
	// ErrInvalidVessel 渔船登记信息非法。
	ErrInvalidVessel = errors.New("model: 渔船登记信息非法")
	// ErrInvalidCatch 卸鱼记录非法。
	ErrInvalidCatch = errors.New("model: 卸鱼记录非法")
	// ErrVesselUnknown 渔船未登记。
	ErrVesselUnknown = errors.New("model: 渔船未登记")
	// ErrVesselRetired 渔船已注销。
	ErrVesselRetired = errors.New("model: 渔船已注销")
	// ErrPortUnknown 渔港不存在。
	ErrPortUnknown = errors.New("model: 渔港不存在")
	// ErrPermitUnknown 许可不存在。
	ErrPermitUnknown = errors.New("model: 出港许可不存在")
	// ErrDuplicateVessel 渔船重复登记。
	ErrDuplicateVessel = errors.New("model: 渔船重复登记")
	// ErrSeasonClosed 处于伏季休渔期，禁止出港作业。
	ErrSeasonClosed = errors.New("model: 伏季休渔期内禁止出港")
	// ErrStateConflict 状态流转非法。
	ErrStateConflict = errors.New("model: 许可状态流转非法")
	// ErrQuotaExceeded 配额超限。
	ErrQuotaExceeded = errors.New("model: 捕捞配额超限")
	// ErrNoBerth 泊位不足。
	ErrNoBerth = errors.New("model: 渔港泊位不足")
	// ErrNoSeasonWindow 该海区不实行伏季休渔。
	ErrNoSeasonWindow = errors.New("model: 该海区不实行伏季休渔")
)
