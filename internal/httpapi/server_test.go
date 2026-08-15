package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nanhaiport/internal/model"
	"nanhaiport/internal/permit"
	"nanhaiport/internal/season"
	"nanhaiport/internal/seed"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	reg, ledger, err := seed.Load()
	if err != nil {
		t.Fatalf("seed.Load 失败: %v", err)
	}
	cal := season.NewCalendar()
	svc := permit.NewService(reg, cal, ledger)
	fixedNow, err := time.Parse(time.RFC3339, "2026-08-18T09:00:00+08:00")
	if err != nil {
		t.Fatalf("解析时间失败: %v", err)
	}
	srv := New(Options{Registry: reg, Calendar: cal, Ledger: ledger, Permits: svc, Now: func() time.Time { return fixedNow }})
	return srv.Handler()
}

func doJSON(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var payload map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s %s 响应不是合法 JSON: %v\n%s", method, path, err, rec.Body.String())
		}
	}
	return rec, payload
}

func errorCode(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, ok := payload["error"]
	if !ok {
		t.Fatalf("响应中缺少 error 字段: %+v", payload)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("error 字段格式异常: %+v", raw)
	}
	code, _ := obj["code"].(string)
	return code
}

func TestHealth(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if payload["status"] != "ok" {
		t.Fatalf("响应 = %+v", payload)
	}
}

func TestSeasonStatusEndpoint(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/season/status?zone=nanhai-north&at=2026-06-20T09:00:00%2B08:00", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if payload["phase"] != "closed" {
		t.Fatalf("phase = %v, 期望 closed", payload["phase"])
	}
}

func TestSeasonStatusUnknownZone(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/season/status?zone=donghai", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodeBadRequest) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeBadRequest)
	}
}

func TestVesselNotFound(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/vessels/NOPE-9", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodeVesselUnknown) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeVesselUnknown)
	}
}

func TestQuotaSummaryEndpoint(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/quota/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if got, _ := payload["total_quota"].(float64); got != seed.TotalAllocatedTonnes() {
		t.Fatalf("total_quota = %v, 期望 %v", payload["total_quota"], seed.TotalAllocatedTonnes())
	}
}

// TestSeasonReportEndpointTotals 断言休渔报表接口返回的配额合计与台账登记一致。
func TestSeasonReportEndpointTotals(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/report/season?at=2026-08-18T09:00:00%2B08:00", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	got, _ := payload["total_quota"].(float64)
	if got != seed.TotalAllocatedTonnes() {
		t.Fatalf("报表 total_quota = %v, 期望 %v", payload["total_quota"], seed.TotalAllocatedTonnes())
	}
}

// TestQuotaSummaryStableAfterVesselQuery 断言先查单船配额再查汇总，汇总结果不变。
func TestQuotaSummaryStableAfterVesselQuery(t *testing.T) {
	h := newTestServer(t)
	_, before := doJSON(t, h, http.MethodGet, "/api/quota/summary", "")
	if rec, _ := doJSON(t, h, http.MethodGet, "/api/quota/vessels/YJ-001", ""); rec.Code != http.StatusOK {
		t.Fatalf("单船配额查询状态码 = %d, 期望 200", rec.Code)
	}
	_, after := doJSON(t, h, http.MethodGet, "/api/quota/summary", "")
	if before["total_quota"] != after["total_quota"] {
		t.Fatalf("单船查询后 total_quota 从 %v 变为 %v", before["total_quota"], after["total_quota"])
	}
	if before["allocations"] != after["allocations"] {
		t.Fatalf("单船查询后 allocations 从 %v 变为 %v", before["allocations"], after["allocations"])
	}
}

func createPermit(t *testing.T, h http.Handler, vesselID, departAt string) string {
	t.Helper()
	body := `{"vessel_id":"` + vesselID + `","depart_at":"` + departAt + `","operator":"港长"}`
	rec, payload := doJSON(t, h, http.MethodPost, "/api/permits", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建许可状态码 = %d, 期望 201, 响应 %+v", rec.Code, payload)
	}
	id, _ := payload["id"].(string)
	if id == "" {
		t.Fatalf("创建许可未返回 id: %+v", payload)
	}
	return id
}

func advance(t *testing.T, h http.Handler, id, action string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	body := `{"action":"` + action + `","operator":"渔政"}`
	return doJSON(t, h, http.MethodPost, "/api/permits/"+id+"/actions", body)
}

// TestApproveDuringClosureReturnsConflict 断言休渔期内核准出港许可返回 409 与 season_closed 错误码。
func TestApproveDuringClosureReturnsConflict(t *testing.T) {
	h := newTestServer(t)
	id := createPermit(t, h, "YJ-001", "2026-06-20T05:00:00+08:00")
	if rec, payload := advance(t, h, id, "submit"); rec.Code != http.StatusOK {
		t.Fatalf("submit 状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	if rec, payload := advance(t, h, id, "review"); rec.Code != http.StatusOK {
		t.Fatalf("review 状态码 = %d, 响应 %+v", rec.Code, payload)
	}
	rec, payload := advance(t, h, id, "approve")
	if rec.Code != http.StatusConflict {
		t.Fatalf("休渔期内核准状态码 = %d, 期望 409, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeSeasonClosed) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeSeasonClosed)
	}
}

// TestStateConflictReturnsConflict 断言非法状态流转返回 409 与 state_conflict 错误码。
func TestStateConflictReturnsConflict(t *testing.T) {
	h := newTestServer(t)
	id := createPermit(t, h, "YJ-001", "2026-08-20T05:00:00+08:00")
	rec, payload := advance(t, h, id, "approve")
	if rec.Code != http.StatusConflict {
		t.Fatalf("状态码 = %d, 期望 409, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeStateConflict) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeStateConflict)
	}
}

// TestPermitActionOnUnknownPermitReturnsNotFound 断言许可不存在时返回 404 与 permit_unknown 错误码。
func TestPermitActionOnUnknownPermitReturnsNotFound(t *testing.T) {
	h := newTestServer(t)
	rec, payload := advance(t, h, "P-9999", "submit")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodePermitUnknown) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodePermitUnknown)
	}
}

// TestCreatePermitUnknownVesselReturnsNotFound 断言渔船未登记时返回 404 与 vessel_unknown 错误码。
func TestCreatePermitUnknownVesselReturnsNotFound(t *testing.T) {
	h := newTestServer(t)
	body := `{"vessel_id":"NOPE-1","depart_at":"2026-08-20T05:00:00+08:00","operator":"港长"}`
	rec, payload := doJSON(t, h, http.MethodPost, "/api/permits", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态码 = %d, 期望 404, 响应 %+v", rec.Code, payload)
	}
	if got := errorCode(t, payload); got != string(CodeVesselUnknown) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodeVesselUnknown)
	}
}

// TestApproveAtReopeningInstantSucceeds 断言开渔时刻的出港许可可以核准通过。
func TestApproveAtReopeningInstantSucceeds(t *testing.T) {
	h := newTestServer(t)
	id := createPermit(t, h, "YJ-001", "2026-08-16T12:00:00+08:00")
	for _, action := range []string{"submit", "review", "approve"} {
		rec, payload := advance(t, h, id, action)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d, 期望 200, 响应 %+v", action, rec.Code, payload)
		}
	}
	rec, payload := doJSON(t, h, http.MethodGet, "/api/permits/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("查询状态码 = %d", rec.Code)
	}
	if payload["state"] != "approved" {
		t.Fatalf("许可状态 = %v, 期望 approved", payload["state"])
	}
}

func TestApproveExemptClassDuringClosure(t *testing.T) {
	h := newTestServer(t)
	id := createPermit(t, h, "YJ-004", "2026-06-20T05:00:00+08:00")
	for _, action := range []string{"submit", "review", "approve"} {
		rec, payload := advance(t, h, id, action)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d, 期望 200, 响应 %+v", action, rec.Code, payload)
		}
	}
}

func TestBadRequestBodies(t *testing.T) {
	h := newTestServer(t)
	rec, _ := doJSON(t, h, http.MethodPost, "/api/permits", `{"vessel_id":"YJ-001","depart_at":"not-a-time"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法时间状态码 = %d, 期望 400", rec.Code)
	}
	rec, _ = doJSON(t, h, http.MethodPost, "/api/permits/P-0001/actions", `{"action":"teleport"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法动作状态码 = %d, 期望 400", rec.Code)
	}
}

func TestZonesEndpoint(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/zones?at=2026-08-16T12:00:00%2B08:00", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	zones, ok := payload["zones"].([]any)
	if !ok || len(zones) != 4 {
		t.Fatalf("zones = %+v", payload["zones"])
	}
}

func TestPortsAndVesselsEndpoint(t *testing.T) {
	h := newTestServer(t)
	rec, payload := doJSON(t, h, http.MethodGet, "/api/ports", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if ports, ok := payload["ports"].([]any); !ok || len(ports) != len(seed.Ports()) {
		t.Fatalf("ports = %+v", payload["ports"])
	}

	rec, payload = doJSON(t, h, http.MethodGet, "/api/vessels?port=zhapo", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	if vessels, ok := payload["vessels"].([]any); !ok || len(vessels) != 4 {
		t.Fatalf("vessels = %+v", payload["vessels"])
	}

	rec, payload = doJSON(t, h, http.MethodGet, "/api/vessels?port=nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未知渔港状态码 = %d, 期望 404", rec.Code)
	}
	if got := errorCode(t, payload); got != string(CodePortUnknown) {
		t.Fatalf("错误码 = %q, 期望 %q", got, CodePortUnknown)
	}
}

func TestClassifyMapsSentinels(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{fmt.Errorf("上层包装: %w", model.ErrSeasonClosed), http.StatusConflict, CodeSeasonClosed},
		{fmt.Errorf("上层包装: %w", model.ErrVesselUnknown), http.StatusNotFound, CodeVesselUnknown},
		{fmt.Errorf("上层包装: %w", model.ErrStateConflict), http.StatusConflict, CodeStateConflict},
		{fmt.Errorf("上层包装: %w", model.ErrQuotaExceeded), http.StatusConflict, CodeQuotaExceeded},
		{fmt.Errorf("上层包装: %w", model.ErrUnknownZone), http.StatusBadRequest, CodeBadRequest},
		{fmt.Errorf("上层包装: %w", context.Canceled), http.StatusServiceUnavailable, CodeUnavailable},
		{errors.New("未归类"), http.StatusInternalServerError, CodeInternal},
	}
	for _, tc := range cases {
		status, code := Classify(tc.err)
		if status != tc.wantStatus || code != tc.wantCode {
			t.Errorf("Classify(%v) = %d/%s, 期望 %d/%s", tc.err, status, code, tc.wantStatus, tc.wantCode)
		}
	}
}
