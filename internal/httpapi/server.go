// Package httpapi 提供渔港调度平台的只读查询与许可审批 HTTP 接口。
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nanhaiport/internal/model"
	"nanhaiport/internal/permit"
	"nanhaiport/internal/quota"
	"nanhaiport/internal/registry"
	"nanhaiport/internal/report"
	"nanhaiport/internal/season"
)

// Server 是平台 HTTP 服务。
type Server struct {
	registry *registry.Registry
	calendar *season.Calendar
	ledger   *quota.Ledger
	permits  *permit.Service
	reports  *report.Builder
	now      func() time.Time
}

// Options 是构造 Server 所需的依赖。
type Options struct {
	Registry *registry.Registry
	Calendar *season.Calendar
	Ledger   *quota.Ledger
	Permits  *permit.Service
	Now      func() time.Time
}

// New 构造 HTTP 服务。
func New(opts Options) *Server {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Server{
		registry: opts.Registry,
		calendar: opts.Calendar,
		ledger:   opts.Ledger,
		permits:  opts.Permits,
		reports:  report.NewBuilder(opts.Registry, opts.Calendar, opts.Ledger, opts.Permits),
		now:      now,
	}
}

// Handler 返回注册好全部路由的处理器。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/ports", s.handlePorts)
	mux.HandleFunc("GET /api/vessels", s.handleVessels)
	mux.HandleFunc("GET /api/vessels/{id}", s.handleVessel)
	mux.HandleFunc("GET /api/zones", s.handleZones)
	mux.HandleFunc("GET /api/season/status", s.handleSeasonStatus)
	mux.HandleFunc("GET /api/quota/summary", s.handleQuotaSummary)
	mux.HandleFunc("GET /api/quota/vessels/{id}", s.handleQuotaVessel)
	mux.HandleFunc("GET /api/report/season", s.handleSeasonReport)
	mux.HandleFunc("GET /api/permits", s.handlePermitList)
	mux.HandleFunc("GET /api/permits/{id}", s.handlePermitGet)
	mux.HandleFunc("POST /api/permits", s.handlePermitCreate)
	mux.HandleFunc("POST /api/permits/{id}/actions", s.handlePermitAction)
	return mux
}

type errorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status, code := Classify(err)
	s.writeJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: err.Error()}})
}

func (s *Server) parseAt(r *http.Request) (time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("at"))
	if raw == "" {
		return s.now(), nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("httpapi: at 参数需为 RFC3339 时间: %q", raw)
	}
	return parsed, nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	counts := s.registry.Counts()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "nanhaiport",
		"ports":   counts.Ports,
		"vessels": counts.Vessels,
	})
}

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"ports": s.registry.Ports()})
}

func (s *Server) handleVessels(w http.ResponseWriter, r *http.Request) {
	port := strings.TrimSpace(r.URL.Query().Get("port"))
	if port != "" {
		if _, err := s.registry.Port(port); err != nil {
			s.writeError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"vessels": s.registry.VesselsByPort(port)})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"vessels": s.registry.Vessels()})
}

func (s *Server) handleVessel(w http.ResponseWriter, r *http.Request) {
	at, err := s.parseAt(r)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
		return
	}
	line, err := s.reports.Vessel(at, r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, line)
}

func (s *Server) handleZones(w http.ResponseWriter, r *http.Request) {
	at, err := s.parseAt(r)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
		return
	}
	lines, err := s.reports.Zones(at)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"zones": lines})
}

func (s *Server) handleSeasonStatus(w http.ResponseWriter, r *http.Request) {
	at, err := s.parseAt(r)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
		return
	}
	zoneRaw := strings.TrimSpace(r.URL.Query().Get("zone"))
	if zoneRaw == "" {
		zoneRaw = string(model.ZoneNanhaiNorth)
	}
	zone, err := model.ParseZone(zoneRaw)
	if err != nil {
		s.writeError(w, err)
		return
	}
	st, err := s.calendar.StatusAt(at, zone)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleQuotaSummary(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.ledger.Summary())
}

func (s *Server) handleQuotaVessel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.registry.Vessel(id); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"vessel_id":   id,
		"allocations": s.ledger.VesselAllocations(id),
		"usage":       s.ledger.UsageFor(id),
	})
}

func (s *Server) handleSeasonReport(w http.ResponseWriter, r *http.Request) {
	at, err := s.parseAt(r)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
		return
	}
	rep, err := s.reports.Season(at)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rep)
}

func (s *Server) handlePermitList(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state != "" {
		s.writeJSON(w, http.StatusOK, map[string]any{"permits": s.permits.ListByState(model.PermitState(state))})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"permits": s.permits.List()})
}

func (s *Server) handlePermitGet(w http.ResponseWriter, r *http.Request) {
	p, err := s.permits.Get(r.PathValue("id"))
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

type createPermitRequest struct {
	VesselID string `json:"vessel_id"`
	DepartAt string `json:"depart_at"`
	ReturnBy string `json:"return_by"`
	Operator string `json:"operator"`
}

func (s *Server) handlePermitCreate(w http.ResponseWriter, r *http.Request) {
	var body createPermitRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: "httpapi: 请求体不是合法 JSON"}})
		return
	}
	departAt, err := parseRequired(body.DepartAt, "depart_at")
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
		return
	}
	var returnBy time.Time
	if strings.TrimSpace(body.ReturnBy) != "" {
		returnBy, err = parseRequired(body.ReturnBy, "return_by")
		if err != nil {
			s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
			return
		}
	}
	p, err := s.permits.Create(permit.Request{
		VesselID: body.VesselID,
		DepartAt: departAt,
		ReturnBy: returnBy,
		Operator: body.Operator,
	})
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, p)
}

type permitActionRequest struct {
	Action   string `json:"action"`
	Operator string `json:"operator"`
	Note     string `json:"note"`
	At       string `json:"at"`
}

func (s *Server) handlePermitAction(w http.ResponseWriter, r *http.Request) {
	var body permitActionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: "httpapi: 请求体不是合法 JSON"}})
		return
	}
	action, err := permit.ParseAction(strings.TrimSpace(body.Action))
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
		return
	}
	at := s.now()
	if strings.TrimSpace(body.At) != "" {
		at, err = parseRequired(body.At, "at")
		if err != nil {
			s.writeJSON(w, http.StatusBadRequest, errorEnvelope{Error: errorBody{Code: CodeBadRequest, Message: err.Error()}})
			return
		}
	}
	p, err := s.permits.Advance(r.Context(), r.PathValue("id"), action, body.Operator, body.Note, at)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

func parseRequired(raw, field string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("httpapi: %s 需为 RFC3339 时间: %q", field, raw)
	}
	return parsed, nil
}
