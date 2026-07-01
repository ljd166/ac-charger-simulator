# AC Charger Simulator

OCPP 1.6J 多桩交流充电桩模拟器，用于场站控制器常规测试。

## 功能

- 支持多台模拟 AC 桩同时运行
- 完整的 OCPP 1.6J 基础链路
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

## 版本

v0.2.0 MVP
