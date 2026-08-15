// Package cli 实现 portctl 命令行界面。
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"nanhaiport/internal/dispatch"
	"nanhaiport/internal/httpapi"
	"nanhaiport/internal/model"
	"nanhaiport/internal/permit"
	"nanhaiport/internal/quota"
	"nanhaiport/internal/registry"
	"nanhaiport/internal/report"
	"nanhaiport/internal/season"
	"nanhaiport/internal/seed"
	"nanhaiport/internal/store"
)

// 退出码约定。上层脚本依赖这些取值区分失败类别。
const (
	// ExitOK 正常结束。
	ExitOK = 0
	// ExitUsage 命令行用法错误或未归类的内部错误。
	ExitUsage = 1
	// ExitBadRequest 参数非法。
	ExitBadRequest = 2
	// ExitConflict 业务冲突：休渔期限制、状态流转冲突、配额超限等。
	ExitConflict = 3
	// ExitAborted 调度被取消或超时。
	ExitAborted = 4
	// ExitNotFound 资源不存在。
	ExitNotFound = 5
)

const usage = `portctl —— 南海伏季休渔渔港调度平台命令行

用法:
  portctl <命令> [子命令] [参数]

命令:
  season status      查询指定时刻、指定海区的休渔状态
  season countdown   查询距开渔剩余天数
  port list          列出渔港
  vessel list        列出渔船
  vessel show        查询单船休渔与配额情况
  quota summary      输出配额台账汇总
  quota audit        先按渔船查询配额，再输出台账汇总
  report season      生成休渔报表
  report zones       生成海区报表
  permit run         走完一份出港许可的提交、初审、核准流程
  dispatch run       批量执行出港许可审批
  journal replay     回放调度流水文件
  serve              启动 HTTP 服务
  selfcheck          运行内置自检
  version            输出版本信息

退出码:
  0 成功  1 用法或内部错误  2 参数非法  3 业务冲突  4 调度中止  5 资源不存在
`

// Version 是当前构建版本。
const Version = "0.4.0"

type app struct {
	registry *registry.Registry
	ledger   *quota.Ledger
	calendar *season.Calendar
	permits  *permit.Service
	reports  *report.Builder
	journal  *store.Journal
	stdout   io.Writer
	stderr   io.Writer
}

func newApp(stdout, stderr io.Writer) (*app, error) {
	reg, ledger, err := seed.Load()
	if err != nil {
		return nil, err
	}
	cal := season.NewCalendar()
	svc := permit.NewService(reg, cal, ledger)
	return &app{
		registry: reg,
		ledger:   ledger,
		calendar: cal,
		permits:  svc,
		reports:  report.NewBuilder(reg, cal, ledger, svc),
		journal:  store.NewJournal(),
		stdout:   stdout,
		stderr:   stderr,
	}, nil
}

// Run 执行一次命令行调用并返回退出码。
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	a, err := newApp(stdout, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "初始化失败: %v\n", err)
		return ExitUsage
	}
	code, err := a.dispatchCommand(args)
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
	}
	return code
}

func (a *app) dispatchCommand(args []string) (int, error) {
	switch args[0] {
	case "version":
		fmt.Fprintf(a.stdout, "portctl %s\n", Version)
		return ExitOK, nil
	case "season":
		return a.runSeason(args[1:])
	case "port":
		return a.runPort(args[1:])
	case "vessel":
		return a.runVessel(args[1:])
	case "quota":
		return a.runQuota(args[1:])
	case "report":
		return a.runReport(args[1:])
	case "permit":
		return a.runPermit(args[1:])
	case "dispatch":
		return a.runDispatch(args[1:])
	case "journal":
		return a.runJournal(args[1:])
	case "serve":
		return a.runServe(args[1:])
	case "selfcheck":
		return a.runSelfcheck(args[1:])
	default:
		fmt.Fprint(a.stderr, usage)
		return ExitUsage, fmt.Errorf("未知命令 %q", args[0])
	}
}

func (a *app) emit(payload any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// classify 把领域错误映射为退出码，映射依据是错误链中的哨兵错误。
func classify(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ExitAborted
	case errors.Is(err, model.ErrSeasonClosed),
		errors.Is(err, model.ErrStateConflict),
		errors.Is(err, model.ErrQuotaExceeded),
		errors.Is(err, model.ErrDuplicateVessel),
		errors.Is(err, model.ErrVesselRetired),
		errors.Is(err, model.ErrNoBerth):
		return ExitConflict
	case errors.Is(err, model.ErrVesselUnknown),
		errors.Is(err, model.ErrPortUnknown),
		errors.Is(err, model.ErrPermitUnknown):
		return ExitNotFound
	case errors.Is(err, model.ErrUnknownZone),
		errors.Is(err, model.ErrUnknownVesselClass),
		errors.Is(err, model.ErrInvalidVessel),
		errors.Is(err, model.ErrInvalidCatch),
		errors.Is(err, model.ErrNoSeasonWindow):
		return ExitBadRequest
	default:
		return ExitUsage
	}
}

func parseAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now(), nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("--at 需为 RFC3339 时间, 收到 %q", raw)
	}
	return parsed, nil
}

func (a *app) runSeason(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("season 需要子命令: status 或 countdown")
	}
	fs := flag.NewFlagSet("season "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	zoneFlag := fs.String("zone", string(model.ZoneNanhaiNorth), "海区代码")
	atFlag := fs.String("at", "", "查询时刻, RFC3339")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	at, err := parseAt(*atFlag)
	if err != nil {
		return ExitBadRequest, err
	}
	zone, err := model.ParseZone(*zoneFlag)
	if err != nil {
		return classify(err), err
	}
	st, err := a.calendar.StatusAt(at, zone)
	if err != nil {
		return classify(err), err
	}

	switch args[0] {
	case "status":
		payload := map[string]any{
			"zone":           string(zone),
			"zone_name":      zone.DisplayName(),
			"at":             at.In(season.Beijing()).Format(time.RFC3339),
			"phase":          string(st.Phase),
			"phase_name":     st.Phase.DisplayName(),
			"closed":         st.Closed(),
			"remaining_days": st.RemainingDays,
		}
		if st.Window != nil {
			payload["window"] = st.Window.Format()
		}
		return ExitOK, a.emit(payload)
	case "countdown":
		payload := map[string]any{
			"zone":           string(zone),
			"phase":          string(st.Phase),
			"remaining_days": st.RemainingDays,
		}
		if st.Window != nil {
			payload["reopen_at"] = st.Window.End.In(season.Beijing()).Format(time.RFC3339)
		}
		return ExitOK, a.emit(payload)
	default:
		return ExitUsage, fmt.Errorf("未知子命令 season %q", args[0])
	}
}

func (a *app) runPort(args []string) (int, error) {
	if len(args) == 0 || args[0] != "list" {
		return ExitUsage, errors.New("port 需要子命令: list")
	}
	return ExitOK, a.emit(map[string]any{"ports": a.registry.Ports()})
}

func (a *app) runVessel(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("vessel 需要子命令: list 或 show")
	}
	fs := flag.NewFlagSet("vessel "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	portFlag := fs.String("port", "", "按渔港筛选")
	idFlag := fs.String("id", "", "渔船编号")
	atFlag := fs.String("at", "", "查询时刻, RFC3339")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	switch args[0] {
	case "list":
		if *portFlag != "" {
			if _, err := a.registry.Port(*portFlag); err != nil {
				return classify(err), err
			}
			return ExitOK, a.emit(map[string]any{"vessels": a.registry.VesselsByPort(*portFlag)})
		}
		return ExitOK, a.emit(map[string]any{"vessels": a.registry.Vessels()})
	case "show":
		if *idFlag == "" {
			return ExitBadRequest, errors.New("vessel show 需要 --id")
		}
		at, err := parseAt(*atFlag)
		if err != nil {
			return ExitBadRequest, err
		}
		line, err := a.reports.Vessel(at, *idFlag)
		if err != nil {
			return classify(err), err
		}
		return ExitOK, a.emit(line)
	default:
		return ExitUsage, fmt.Errorf("未知子命令 vessel %q", args[0])
	}
}

func (a *app) runQuota(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("quota 需要子命令: summary 或 audit")
	}
	fs := flag.NewFlagSet("quota "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	vesselFlag := fs.String("vessel", "", "渔船编号")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	switch args[0] {
	case "summary":
		return ExitOK, a.emit(a.ledger.Summary())
	case "audit":
		if *vesselFlag == "" {
			return ExitBadRequest, errors.New("quota audit 需要 --vessel")
		}
		if _, err := a.registry.Vessel(*vesselFlag); err != nil {
			return classify(err), err
		}
		vesselRows := a.ledger.VesselAllocations(*vesselFlag)
		payload := map[string]any{
			"vessel_id":          *vesselFlag,
			"vessel_quota":       a.ledger.TotalQuotaFor(*vesselFlag),
			"vessel_allocations": vesselRows,
			"ledger_summary":     a.ledger.Summary(),
			"ledger_vessels":     a.ledger.VesselIDs(),
		}
		return ExitOK, a.emit(payload)
	default:
		return ExitUsage, fmt.Errorf("未知子命令 quota %q", args[0])
	}
}

func (a *app) runReport(args []string) (int, error) {
	if len(args) == 0 {
		return ExitUsage, errors.New("report 需要子命令: season 或 zones")
	}
	fs := flag.NewFlagSet("report "+args[0], flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	atFlag := fs.String("at", "", "报表时刻, RFC3339")
	outFlag := fs.String("out", "", "写出 JSON 文件路径")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	at, err := parseAt(*atFlag)
	if err != nil {
		return ExitBadRequest, err
	}
	var payload any
	switch args[0] {
	case "season":
		rep, err := a.reports.Season(at)
		if err != nil {
			return classify(err), err
		}
		payload = rep
	case "zones":
		lines, err := a.reports.Zones(at)
		if err != nil {
			return classify(err), err
		}
		payload = map[string]any{"zones": lines}
	default:
		return ExitUsage, fmt.Errorf("未知子命令 report %q", args[0])
	}
	if *outFlag != "" {
		if err := store.SaveJSON(*outFlag, payload); err != nil {
			return ExitUsage, err
		}
	}
	return ExitOK, a.emit(payload)
}

func (a *app) runPermit(args []string) (int, error) {
	if len(args) == 0 || args[0] != "run" {
		return ExitUsage, errors.New("permit 需要子命令: run")
	}
	fs := flag.NewFlagSet("permit run", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	vesselFlag := fs.String("vessel", "", "渔船编号")
	departFlag := fs.String("depart", "", "计划出港时刻, RFC3339")
	operatorFlag := fs.String("operator", "渔政值班", "操作人")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *vesselFlag == "" || *departFlag == "" {
		return ExitBadRequest, errors.New("permit run 需要 --vessel 和 --depart")
	}
	departAt, err := time.Parse(time.RFC3339, *departFlag)
	if err != nil {
		return ExitBadRequest, fmt.Errorf("--depart 需为 RFC3339 时间, 收到 %q", *departFlag)
	}

	p, err := a.permits.Create(permit.Request{
		VesselID: *vesselFlag,
		DepartAt: departAt,
		ReturnBy: departAt.Add(96 * time.Hour),
		Operator: *operatorFlag,
	})
	if err != nil {
		return classify(err), err
	}
	ctx := context.Background()
	for _, action := range []permit.Action{permit.ActionSubmit, permit.ActionReview, permit.ActionApprove} {
		p, err = a.permits.Advance(ctx, p.ID, action, *operatorFlag, "", departAt)
		if err != nil {
			code := classify(err)
			_ = a.emit(map[string]any{
				"permit_id": p.ID,
				"action":    string(action),
				"ok":        false,
				"exit_code": code,
				"message":   err.Error(),
			})
			return code, err
		}
	}
	return ExitOK, a.emit(map[string]any{
		"permit_id": p.ID,
		"vessel_id": p.VesselID,
		"state":     string(p.State),
		"ok":        true,
		"history":   len(p.History),
	})
}

func (a *app) runDispatch(args []string) (int, error) {
	if len(args) == 0 || args[0] != "run" {
		return ExitUsage, errors.New("dispatch 需要子命令: run")
	}
	fs := flag.NewFlagSet("dispatch run", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	workersFlag := fs.Int("workers", 4, "并发度")
	countFlag := fs.Int("count", 12, "任务数量")
	timeoutFlag := fs.Duration("timeout", 5*time.Second, "整批调度超时")
	latencyFlag := fs.Duration("gateway-latency", 20*time.Millisecond, "单次渔政网关调用耗时")
	departFlag := fs.String("depart", "2026-08-18T05:00:00+08:00", "计划出港时刻, RFC3339")
	journalFlag := fs.String("journal", "", "调度流水落盘路径")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *countFlag <= 0 {
		return ExitBadRequest, errors.New("--count 必须为正")
	}
	departAt, err := time.Parse(time.RFC3339, *departFlag)
	if err != nil {
		return ExitBadRequest, fmt.Errorf("--depart 需为 RFC3339 时间, 收到 %q", *departFlag)
	}

	tasks, err := a.buildDispatchTasks(*countFlag, departAt, *latencyFlag)
	if err != nil {
		return classify(err), err
	}

	scheduler := dispatch.New(*workersFlag, a.journal)
	ctx, cancel := context.WithTimeout(context.Background(), *timeoutFlag)
	defer cancel()

	begin := time.Now()
	rep, runErr := scheduler.Run(ctx, tasks)
	elapsed := time.Since(begin)

	payload := map[string]any{
		"total":       rep.Total,
		"done":        rep.Done,
		"failed":      rep.Failed,
		"skipped":     rep.Skipped,
		"aborted":     rep.Aborted,
		"workers":     rep.Workers,
		"timeout_ms":  timeoutFlag.Milliseconds(),
		"elapsed_ms":  elapsed.Milliseconds(),
		"journal_len": a.journal.Len(),
	}
	if runErr != nil {
		payload["message"] = runErr.Error()
	}
	if *journalFlag != "" {
		if err := a.journal.Flush(*journalFlag); err != nil {
			return ExitUsage, err
		}
		payload["journal_path"] = *journalFlag
	}
	if err := a.emit(payload); err != nil {
		return ExitUsage, err
	}
	if runErr != nil {
		return classify(runErr), runErr
	}
	return ExitOK, nil
}

func (a *app) buildDispatchTasks(count int, departAt time.Time, latency time.Duration) ([]dispatch.Task, error) {
	vessels := make([]model.Vessel, 0)
	for _, v := range a.registry.Vessels() {
		if v.Retired {
			continue
		}
		vessels = append(vessels, v)
	}
	if len(vessels) == 0 {
		return nil, errors.New("登记簿中没有可调度的渔船")
	}
	tasks := make([]dispatch.Task, 0, count)
	for i := 0; i < count; i++ {
		v := vessels[i%len(vessels)]
		p, err := a.permits.Create(permit.Request{
			VesselID: v.ID,
			DepartAt: departAt,
			ReturnBy: departAt.Add(96 * time.Hour),
			Operator: "批量调度",
		})
		if err != nil {
			return nil, err
		}
		permitID := p.ID
		tasks = append(tasks, dispatch.Task{
			ID:      fmt.Sprintf("D-%04d", i+1),
			Subject: permitID,
			Kind:    "permit-approve",
			Run: func(ctx context.Context) error {
				if err := dispatch.GatewayCall(ctx, latency); err != nil {
					return err
				}
				for _, action := range []permit.Action{permit.ActionSubmit, permit.ActionReview} {
					if _, err := a.permits.Advance(ctx, permitID, action, "批量调度", "", departAt); err != nil {
						return err
					}
				}
				_, err := a.permits.Advance(ctx, permitID, permit.ActionApprove, "批量调度", "", departAt)
				return err
			},
		})
	}
	return tasks, nil
}

func (a *app) runJournal(args []string) (int, error) {
	if len(args) == 0 || args[0] != "replay" {
		return ExitUsage, errors.New("journal 需要子命令: replay")
	}
	fs := flag.NewFlagSet("journal replay", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	pathFlag := fs.String("path", "", "流水文件路径")
	if err := fs.Parse(args[1:]); err != nil {
		return ExitUsage, err
	}
	if *pathFlag == "" {
		return ExitBadRequest, errors.New("journal replay 需要 --path")
	}
	entries, err := store.Replay(*pathFlag)
	if err != nil {
		return ExitUsage, err
	}
	failed := 0
	for _, e := range entries {
		if !e.Success {
			failed++
		}
	}
	return ExitOK, a.emit(map[string]any{
		"path":    *pathFlag,
		"entries": len(entries),
		"failed":  failed,
	})
}

func (a *app) runServe(args []string) (int, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	addrFlag := fs.String("addr", "127.0.0.1:8080", "监听地址")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	srv := httpapi.New(httpapi.Options{
		Registry: a.registry,
		Calendar: a.calendar,
		Ledger:   a.ledger,
		Permits:  a.permits,
	})
	fmt.Fprintf(a.stdout, "portctl serve 监听 %s\n", *addrFlag)
	server := &http.Server{
		Addr:              *addrFlag,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return ExitUsage, err
	}
	return ExitOK, nil
}

func (a *app) runSelfcheck(args []string) (int, error) {
	fs := flag.NewFlagSet("selfcheck", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	atFlag := fs.String("at", "2026-08-18T09:00:00+08:00", "自检时刻, RFC3339")
	if err := fs.Parse(args); err != nil {
		return ExitUsage, err
	}
	at, err := parseAt(*atFlag)
	if err != nil {
		return ExitBadRequest, err
	}

	checks := make([]map[string]any, 0, 6)
	add := func(name string, ok bool, detail string) {
		checks = append(checks, map[string]any{"check": name, "ok": ok, "detail": detail})
	}

	counts := a.registry.Counts()
	add("registry", counts.Ports == len(seed.Ports()) && counts.Vessels == len(seed.Vessels()),
		fmt.Sprintf("渔港 %d 条, 渔船 %d 条", counts.Ports, counts.Vessels))

	rep, err := a.reports.Season(at)
	if err != nil {
		return classify(err), err
	}
	wantQuota := seed.TotalAllocatedTonnes()
	add("quota-total", rep.TotalQuota == wantQuota,
		fmt.Sprintf("报表合计 %.2f 吨, 台账登记 %.2f 吨", rep.TotalQuota, wantQuota))
	add("quota-summary", rep.QuotaSummary.Allocations == len(seed.Allocations()),
		fmt.Sprintf("台账配额 %d 条, 期望 %d 条", rep.QuotaSummary.Allocations, len(seed.Allocations())))

	reopen, err := a.calendar.NextOpening(at, model.ZoneNanhaiNorth)
	if err != nil {
		return classify(err), err
	}
	add("season-window", !reopen.IsZero(),
		fmt.Sprintf("南海北部下一次开渔 %s", reopen.In(season.Beijing()).Format(time.RFC3339)))

	openAt := time.Date(2026, time.August, 16, 12, 0, 0, 0, season.Beijing())
	mayDepart, err := a.calendar.MayDepart(openAt, model.ZoneNanhaiNorth, model.ClassTrawler)
	if err != nil {
		return classify(err), err
	}
	add("reopen-instant", mayDepart, fmt.Sprintf("开渔时刻拖网船可出港 = %v", mayDepart))

	zones, err := a.reports.Zones(at)
	if err != nil {
		return classify(err), err
	}
	var zoneQuota float64
	for _, z := range zones {
		zoneQuota += z.QuotaTonnes
	}
	add("zone-total", zoneQuota == wantQuota,
		fmt.Sprintf("海区合计 %.2f 吨, 台账登记 %.2f 吨", zoneQuota, wantQuota))

	sort.Slice(checks, func(i, j int) bool {
		return checks[i]["check"].(string) < checks[j]["check"].(string)
	})
	failed := 0
	for _, c := range checks {
		if !c["ok"].(bool) {
			failed++
		}
	}
	if err := a.emit(map[string]any{"checks": checks, "failed": failed, "at": at.In(season.Beijing()).Format(time.RFC3339)}); err != nil {
		return ExitUsage, err
	}
	if failed > 0 {
		return ExitUsage, fmt.Errorf("自检失败 %d 项", failed)
	}
	return ExitOK, nil
}
