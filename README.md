# nanhaiport —— 南海伏季休渔渔港调度平台

`nanhaiport` 是一个纯 Go 实现的后端与命令行工具，面向南海海域（北纬 12 度以北）
沿岸渔港的伏季休渔期管理，覆盖休渔日历、渔船登记、出港许可审批、捕捞配额台账、
批量调度与统计报表。

项目不依赖任何第三方模块，只使用 Go 标准库。

## 业务背景

南海伏季休渔按海区分别规定起止时刻（均为北京时间）：

| 海区 | 代码 | 休渔起 | 开渔时刻 |
| --- | --- | --- | --- |
| 南海北部 | `nanhai-north` | 5 月 1 日 12:00 | 8 月 16 日 12:00 |
| 北部湾 | `beibu-gulf` | 5 月 1 日 12:00 | 8 月 16 日 12:00 |
| 琼州海峡 | `qiongzhou` | 5 月 1 日 12:00 | 8 月 1 日 12:00 |
| 南海南部 | `nanhai-south` | 不实行伏季休渔 | — |

休渔窗口的起点为闭区间、终点为开区间：终点即当年开渔时刻，到达该时刻起
渔船即可正常出港。单船钓与定置张网两类作业不受休渔限制。

## 目录结构

```
cmd/portctl            命令行入口
internal/model         领域模型与哨兵错误
internal/season        休渔日历与窗口计算
internal/registry      渔船与渔港登记簿
internal/quota         捕捞配额台账
internal/permit        出港许可状态机与审批服务
internal/dispatch      批量审批并发调度器
internal/report        休渔与开渔期统计报表
internal/httpapi       HTTP 查询与审批接口
internal/store         调度流水落盘与回放
internal/seed          内置样例数据
internal/cli           portctl 命令实现
```

## 构建与测试

```bash
export GOTOOLCHAIN=local

go build ./...
go test ./...
go test -race ./...

make build          # 产出 bin/portctl
make selfcheck      # 构建并运行内置自检
```

## 命令行用法

```bash
portctl season status --zone nanhai-north --at 2026-08-16T12:00:00+08:00
portctl season countdown --zone beibu-gulf --at 2026-08-10T09:00:00+08:00

portctl port list
portctl vessel list --port zhapo
portctl vessel show --id YJ-001 --at 2026-08-18T09:00:00+08:00

portctl quota summary
portctl quota audit --vessel YJ-001

portctl report season --at 2026-08-18T09:00:00+08:00
portctl report zones  --at 2026-08-16T12:00:00+08:00

portctl permit run --vessel YJ-001 --depart 2026-08-18T05:00:00+08:00

portctl dispatch run --workers 4 --count 12 --timeout 5s --gateway-latency 20ms
portctl dispatch run --workers 3 --count 12 --timeout 300ms --gateway-latency 5s
portctl journal replay --path ./tmp/journal.jsonl

portctl serve --addr 127.0.0.1:8080
portctl selfcheck
```

### 退出码

| 退出码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 用法错误或未归类的内部错误 |
| 2 | 参数非法 |
| 3 | 业务冲突（休渔期限制、状态流转冲突、配额超限等） |
| 4 | 批量调度被取消或超时 |
| 5 | 资源不存在 |

## HTTP 接口

`portctl serve` 启动只读查询与许可审批接口：

```
GET  /healthz
GET  /api/ports
GET  /api/vessels[?port=zhapo]
GET  /api/vessels/{id}[?at=RFC3339]
GET  /api/zones[?at=RFC3339]
GET  /api/season/status?zone=nanhai-north[&at=RFC3339]
GET  /api/quota/summary
GET  /api/quota/vessels/{id}
GET  /api/report/season[?at=RFC3339]
GET  /api/permits[?state=approved]
GET  /api/permits/{id}
POST /api/permits
POST /api/permits/{id}/actions
```

错误响应统一为：

```json
{ "error": { "code": "season_closed", "message": "..." } }
```

状态码约定：`404` 资源不存在，`409` 业务冲突，`400` 参数非法，
`503` 调用被取消或超时，`500` 未归类的内部错误。

> 说明：HTTP 接口默认不带鉴权，仅面向内网或本地演练环境。若需暴露到公网，
> 必须在前置网关补充身份认证与访问控制。

## 出港许可状态机

```
draft ──submit──> submitted ──review──> reviewed ──approve──> approved
  │                   │                    │                    │
  └──reject──┐        └──reject──┐         └──reject──┐         ├──suspend──> suspended
             ▼                   ▼                    ▼         └──close────> closed
          rejected            rejected             rejected

suspended ──resume──> approved
suspended ──close───> closed
suspended ──reject──> rejected
```

核准（`approve`）动作会附加校验：渔船有效、海区已开渔或作业类型豁免、
渔港有泊位、渔船已获得配额。

## 容器运行

```bash
docker build -t nanhaiport:local .
docker run --rm nanhaiport:local selfcheck
docker run --rm -p 8080:8080 nanhaiport:local serve --addr 0.0.0.0:8080
```

镜像基于 `golang:1.22` 构建、`distroless/static` 运行，同时支持
`linux/amd64` 与 `linux/arm64`：

```bash
docker build --platform linux/amd64 -t nanhaiport:amd64 .
docker build --platform linux/arm64 -t nanhaiport:arm64 .
```

## 数据来源

内置样例数据（`internal/seed`）覆盖广东、广西、海南三省区 6 个渔港、
14 条渔船、17 条配额与 6 条卸鱼记录，仅用于本地演练，不代表真实生产数据。
