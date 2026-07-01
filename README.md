# AC Charger Simulator

OCPP 1.6J 多桩交流充电桩模拟器，用于场站控制器常规测试。

## 功能

- 支持多台模拟 AC 桩同时运行
- 完整的 OCPP 1.6J 基础链路：BootNotification、Heartbeat、StatusNotification、Authorize、StartTransaction、MeterValues、StopTransaction
- 响应 `SetChargingProfile` 限流并调整 `MeterValues`
- 本地 Web Console 实时查看状态、电流/功率曲线
- 测试场景自动化运行

## 快速启动

```bash
# 单条命令启动模拟器 + Web Console（默认 http://127.0.0.1:8088）
go run ./cmd/simulator -config testdata/config-2chargers.yaml -web

# 或只启动模拟器 CLI
go run ./cmd/simulator -config testdata/config-2chargers.yaml
```

浏览器打开 `http://127.0.0.1:8088` 即可看到 Web Console。

## 配置

编辑 `testdata/config-2chargers.yaml` 或 `testdata/config-8chargers.yaml`，修改 `endpoint` 为实际 OCPP 服务器地址。

示例配置：

```yaml
web_console:
  enabled: true
  bind_addr: "127.0.0.1"
  port: 8088
  history_window_sec: 600
chargers:
  - id: "SIM-AC-001"
    connector_id: 1
    endpoint: "ws://127.0.0.1:9000/ocpp/SIM-AC-001"
    id_tag: "TEST-CARD-001"
    phase: "single"
    phase_assignment: "L1"
    max_current_a: 32
    voltage_v: 230
    power_factor: 0.98
    meter_interval_sec: 5
```

## 目录结构

```
cmd/simulator        CLI 入口
internal/config      YAML 配置加载与校验
internal/ocpp16      OCPP 1.6J 消息编解码
internal/charger     单桩状态机
internal/meter       电表模型（电流、电压、功率、电量）
internal/manager     模拟器管理器（多桩聚合）
internal/scenario    测试场景编排
internal/report      Markdown/JSON 测试报告
internal/webconsole  HTTP 静态页面 + API 路由
internal/telemetry   实时状态汇总与 WebSocket 推送
web/static           Web Console 前端
testdata             示例配置
```

## 测试

```bash
go test ./...
go vet ./...
```

## OCPP 1.6J 支持消息

Charge Point -> Central System:
- BootNotification
- Heartbeat
- StatusNotification
- Authorize
- StartTransaction
- MeterValues
- StopTransaction

Central System -> Charge Point:
- RemoteStartTransaction
- RemoteStopTransaction
- SetChargingProfile
- ClearChargingProfile
- Reset
- ChangeAvailability

## Meter Model

- 单相功率：`P = U * I * PF / 1000` (kW)
- 三相功率：`P = 1.732 * U * I * PF / 1000` (kW)
- 收到 `SetChargingProfile(A)` 后目标电流直接生效
- 收到 `SetChargingProfile(W)` 后按相制换算为电流
- 目标电流低于 6A 时自动暂停，记录原因
- 电流在 1-2 个采样周期内收敛到目标值

## Web Console API

- `GET /api/state` — 全局状态
- `POST /api/config/ocpp-endpoint` — 设置 endpoint
- `POST /api/chargers/{id}/connect` — 连接桩
- `POST /api/chargers/{id}/disconnect` — 断开桩
- `POST /api/chargers/{id}/start` — 启动交易
- `POST /api/chargers/{id}/stop` — 停止交易
- `POST /api/chargers/{id}/target-current` — 设置目标电流
- `POST /api/chargers/{id}/fault` — 触发/清除故障
- `POST /api/chargers/{id}/profile` — 切换测试 profile
- `POST /api/chargers/all/start` — 批量启动
- `POST /api/chargers/all/stop` — 批量停止
- `WS /ws/telemetry` — 实时遥测推送

## 开发

项目使用 Go 1.21+，依赖：
- `github.com/gorilla/websocket` — WebSocket 客户端/服务器
- `github.com/gorilla/mux` — HTTP 路由
- `gopkg.in/yaml.v3` — YAML 配置解析

## 测试报告示例

见 `docs/test-report-example.md`。

## 版本

v0.2.0 MVP
