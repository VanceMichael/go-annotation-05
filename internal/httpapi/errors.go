package httpapi

import (
	"context"
	"errors"
	"net/http"

	"nanhaiport/internal/model"
)

// ErrorCode 是对外暴露的机器可读错误码。
type ErrorCode string

const (
	// CodeVesselUnknown 渔船未登记。
	CodeVesselUnknown ErrorCode = "vessel_unknown"
	// CodeVesselRetired 渔船已注销。
	CodeVesselRetired ErrorCode = "vessel_retired"
	// CodePortUnknown 渔港不存在。
	CodePortUnknown ErrorCode = "port_unknown"
	// CodePermitUnknown 许可不存在。
	CodePermitUnknown ErrorCode = "permit_unknown"
	// CodeDuplicateVessel 渔船重复登记。
	CodeDuplicateVessel ErrorCode = "duplicate_vessel"
	// CodeSeasonClosed 休渔期内禁止出港。
	CodeSeasonClosed ErrorCode = "season_closed"
	// CodeStateConflict 状态流转冲突。
	CodeStateConflict ErrorCode = "state_conflict"
	// CodeQuotaExceeded 配额超限。
	CodeQuotaExceeded ErrorCode = "quota_exceeded"
	// CodeNoBerth 泊位不足。
	CodeNoBerth ErrorCode = "no_berth"
	// CodeBadRequest 请求参数非法。
	CodeBadRequest ErrorCode = "bad_request"
	// CodeUnavailable 调度被取消或超时。
	CodeUnavailable ErrorCode = "unavailable"
	// CodeInternal 未归类的内部错误。
	CodeInternal ErrorCode = "internal"
)

// errorMapping 定义哨兵错误到状态码与错误码的映射。
// 顺序决定匹配优先级。
var errorMapping = []struct {
	sentinel error
	status   int
	code     ErrorCode
}{
	{model.ErrVesselRetired, http.StatusConflict, CodeVesselRetired},
	{model.ErrVesselUnknown, http.StatusNotFound, CodeVesselUnknown},
	{model.ErrPortUnknown, http.StatusNotFound, CodePortUnknown},
	{model.ErrPermitUnknown, http.StatusNotFound, CodePermitUnknown},
	{model.ErrDuplicateVessel, http.StatusConflict, CodeDuplicateVessel},
	{model.ErrSeasonClosed, http.StatusConflict, CodeSeasonClosed},
	{model.ErrStateConflict, http.StatusConflict, CodeStateConflict},
	{model.ErrQuotaExceeded, http.StatusConflict, CodeQuotaExceeded},
	{model.ErrNoBerth, http.StatusConflict, CodeNoBerth},
	{model.ErrUnknownZone, http.StatusBadRequest, CodeBadRequest},
	{model.ErrUnknownVesselClass, http.StatusBadRequest, CodeBadRequest},
	{model.ErrInvalidVessel, http.StatusBadRequest, CodeBadRequest},
	{model.ErrInvalidCatch, http.StatusBadRequest, CodeBadRequest},
	{model.ErrNoSeasonWindow, http.StatusBadRequest, CodeBadRequest},
	{context.Canceled, http.StatusServiceUnavailable, CodeUnavailable},
	{context.DeadlineExceeded, http.StatusServiceUnavailable, CodeUnavailable},
}

// Classify 依据错误链把领域错误映射为 HTTP 状态码与错误码。
// 未能识别的错误一律按 500 internal 处理。
func Classify(err error) (int, ErrorCode) {
	if err == nil {
		return http.StatusOK, ""
	}
	for _, m := range errorMapping {
		if errors.Is(err, m.sentinel) {
			return m.status, m.code
		}
	}
	return http.StatusInternalServerError, CodeInternal
}
