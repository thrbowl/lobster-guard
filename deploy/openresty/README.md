# OpenResty 入口模板

这个目录提供第一阶段的 OpenResty 网络入口模板，用来消费 lobster-guard 生成的 `edge-routes.json`，并把 HTTP、SSE、WebSocket 统一转发到 lobster-guard 的 exchange 入口。

## 文件说明

- `nginx.conf`：OpenResty 服务模板，包含 `/readyz`、统一 exchange 转发和 fail-open 分支。
- `lua/edge_routes.lua`：加载并校验 `edge-routes.json`，按 Host、priority、最长 path prefix 匹配路由。
- `edge-routes.json.example`：生成路由文件的示例结构。

## 运行约定

- lobster-guard 是路由源数据的唯一管理方，负责写出一个生成后的 JSON 文件。
- OpenResty 只读取 `edge-routes.json`，不读 DB，也不写 DB。
- 当路由文件缺失、JSON 非法、checksum 校验失败或没有有效路由时，`/readyz` 返回失败。
- 所有业务流量都进入 `LOBSTER_GUARD_EXCHANGE_URL`，由 lobster-guard 再转发到 `upstream_url`。
- `observe` 模式进入主链路，只检测、审计、记录 would-block，但永远放行业务流量。
- `enforce` 模式进入主链路，由 lobster-guard 执行检测、转发、响应检测和阻断。
- `POST /__lobster/exchange` 是统一交换入口。HTTP、SSE、WebSocket 不需要不同入口端点，lobster-guard 在 exchange 内部按协议分支处理。

## 主链路入口

OpenResty 根据 `edge-routes.json` 中每条路由的 `mode` 选择主链路：

| 模式 | 主链路转发目标 | 审计方式 | 是否可拦截 |
|---|---|---|---|
| `observe` | `LOBSTER_GUARD_EXCHANGE_URL` | 主链路记录 HTTP/SSE/WS 流量 | 否，只记录 `would-block` / `would-warn` |
| `enforce` | `LOBSTER_GUARD_EXCHANGE_URL` | 主链路记录完整 HTTP/WS 流量 | 是，由 lobster-guard exchange 入口返回放行、阻断或确认结果 |

统一调用方式：

```text
client -> OpenResty -> /__lobster/exchange -> upstream_url
```

`observe` 和 `enforce` 都走同一个 exchange 主链路。区别只在策略执行结果：`observe` 命中阻断规则也继续放行并记录 would-block；`enforce` 命中阻断规则时，HTTP 返回阻断响应，WebSocket 阻断当前消息。

WebSocket 的默认阻断粒度是单条消息，不是整条连接。`enforce` 命中某条 text frame 时，lobster-guard 丢弃该 frame、返回一条 `lobster_guard_block` 提示消息，并保持连接继续可用。只有认证失败、协议异常、连续攻击超限或策略明确要求关闭时，才应关闭连接。

拦截时 OpenResty 把请求转发到 `LOBSTER_GUARD_EXCHANGE_URL`，由 lobster-guard 在主链路中完成检测、策略判断和上游转发。只有当 exchange 入口连接失败、超时或返回 502/503/504 时，OpenResty 才 fail-open 到该路由的 `upstream_url`。

如果 lobster-guard inline 返回 403、429、confirm 或 block 类响应，OpenResty 必须把该响应直接返回给客户端，不能 bypass 到原始上游。

Lua 模板依赖 `lua-resty-string` 和 `lua-resty-core`。如果基础镜像没有内置这些模块，生产部署前需要安装。

部署前可以设置以下环境变量，也可以直接修改 `nginx.conf`：

- `EDGE_ROUTES_FILE`：路由文件路径，默认 `/etc/lobster-guard/edge-routes.json`。
- `LOBSTER_GUARD_EXCHANGE_URL`：统一交换入口，默认 `http://127.0.0.1:9090/__lobster/exchange`。
- `LOBSTER_GUARD_TOKEN`：调用 exchange 使用的可选 Bearer token。
