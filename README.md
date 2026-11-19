# eino-bot

基于 [cloudwego/eino](https://github.com/cloudwego/eino) ADK 的可持久化个人 Agent Bot。

默认模型走[硅基流动](https://cloud.siliconflow.cn/models) OpenAI 兼容接口；数据层为 **GORM + SQLite**（纯 Go 驱动 `glebarez/sqlite`）。服务默认监听 `127.0.0.1:12120`。

## 已落地能力

### 常驻 HTTP 服务

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 健康检查 |
| GET | `/v1/info` | bot / 模型 / 目录信息 |
| GET | `/v1/chat` | 文字对话（SSE 流式） |
| POST | `/v1/chat` | 文字对话（JSON） |
| POST | `/v1/voice` | 语音上传转写，默认可直接跑 Agent |
| GET | `/v1/profile` | 查询人物画像 |
| GET | `/.well-known/agent-card.json` | 公开 A2A Agent Card |
| POST | `/a2a` | A2A JSON-RPC（`SendMessage` 等） |
| GET | `/v1/a2a/discover` | 兼容发现摘要（指向 Agent Card） |
| POST | `/registry/register` | 注册 Agent Card 到本机目录 |
| POST | `/registry/heartbeat` | Registry 心跳 |
| POST | `/registry/unregister` | 注销 |
| GET | `/registry/agents` | 已注册 Agent 列表（`?skill_tag=人才招聘` 按互动率排序） |
| POST | `/v1/recruit/market/callback` | 市场招聘任务结果回调 |
| GET | `/v1/recruit/market/tasks` | 查询市场招聘任务 |

### 人物记忆与画像

- `remember_fact`：写入原子事实
- 归一化文本 + SHA256 去重（`owner_id + content_hash` 唯一）
- `get_profile` / `search_memory` / `update_profile`
- 对话日志与 ADK checkpoint 落库，进程重启后可续

### 文档 / 简历库

- `analyze_docs`：扫描 `BOT_DOCS_ROOT`（沙箱路径）
- `search_resumes`：扫描 `BOT_RESUME_ROOT`
- 样例：`data/docs/`、`data/resumes/`

### GitHub 线索

- `search_github`：搜用户 / 仓库
- `analyze_github_user`：读公开资料与最近仓库
- 用户不存在（404）等失败时软返回 `found=false` / `ok=false`，不中断整轮
- 可选 `GITHUB_TOKEN` 提高配额

### Bot 间 A2A（公开标准）

基于 [A2A Protocol](https://a2a-protocol.org/latest/specification/) + 官方 Go SDK [`a2aproject/a2a-go`](https://github.com/a2aproject/a2a-go)：

- 发现：`GET /.well-known/agent-card.json`
- 调用：JSON-RPC `SendMessage`（默认路径 `/a2a`）
- 鉴权：可选 `Authorization: Bearer`（`A2A_BEARER_TOKEN`，兼容旧名 `A2A_SHARED_SECRET`）
- Skills：`profile.exchange`、`recruit.inquiry`
- `contact_peer_bot`：先 Resolve Agent Card，再 SendMessage；对方离线返回 `reachable=false`
- **启动自动注册**：默认 `A2A_AUTO_REGISTER=true`，进程起来后把自己写进本机 Registry（或 `A2A_REGISTRY_URL`），并按 `A2A_REGISTRY_HEARTBEAT_SEC` 心跳；退出时注销
- 本机目录：`A2A_HOST_REGISTRY=true` 时提供 `/registry/*` 与 `GET /agents`

### 招聘闭环

1. `save_job_requirement` / `update_job_requirement` 细化并持久化需求  
2. 找人（可并行）：
   - 本地：`search_resumes` / `search_github`
   - **A2A 市场（可选）**：`search_a2a_market_agents`（skill 标签默认「人才招聘」，按互动率排序）→ `start_market_recruit_task` 派任 → 对方回调 `POST /v1/recruit/market/callback` 或 `poll_market_recruit_task` 询问 → `merge_recruit_findings` 与本地扫描合并  
3. `contact_peer_bot` 联系对方助手（可选）  
4. `upsert_candidate` 打分  
5. `rank_top_candidates` / `list_candidates` 标记 Top3  

### Agent 工具一览

| 工具 | 用途 |
| --- | --- |
| `remember_fact` | 写入去重记忆 |
| `get_profile` | 读完整画像 |
| `search_memory` | 关键词检索事实 |
| `update_profile` | 更新显示名/摘要/bot 联系方式 |
| `analyze_docs` | 分析授权文档 |
| `search_resumes` | 检索简历库 |
| `search_github` | GitHub 搜索 |
| `analyze_github_user` | 分析 GitHub 用户 |
| `save_job_requirement` | 保存招聘需求 |
| `update_job_requirement` | 更新招聘需求 |
| `upsert_candidate` | 写入/更新候选人 |
| `list_candidates` | 列出候选人 |
| `rank_top_candidates` | 按分标记 Top N（默认 3） |
| `search_a2a_market_agents` | 按 skill 标签搜人才招聘市场 Agent（互动率优先） |
| `start_market_recruit_task` | 向市场 Agent 发起招聘任务 |
| `poll_market_recruit_task` | 轮询/询问市场任务结果 |
| `merge_recruit_findings` | 合并市场结果与本地简历扫描 |
| `contact_peer_bot` | A2A 信息交换 |

### 工程基础

- 模型：`MODEL_TYPE=siliconflow`（默认）/ `openai` / `ark`
- 需选用支持 Function Calling 的对话模型
- 持久化表：画像事实、画像元数据、对话、checkpoint、招聘需求、候选人、A2A 报文
- 本地联调：`make run`、`make peer|peer2|peer3`
- 单测：记忆去重（`internal/memory`）

## 架构

```
用户文字/语音 ──► Hertz API ──► Eino ChatModelAgent (ReAct)
                                  │
                                  ├─ remember_fact / get_profile / search_memory
                                  ├─ analyze_docs / search_resumes
                                  ├─ search_github / analyze_github_user
                                  ├─ save_job_requirement / upsert_candidate / rank_top_candidates
                                  └─ contact_peer_bot ──► Resolve Agent Card
                                                       └─ JSON-RPC SendMessage (/a2a)
                                                     ▲
                                  GORM + SQLite（画像 / 招聘 / checkpoint / A2A 日志）
```

## 快速开始

```bash
cp .env.example .env
# 填入 SILICONFLOW_API_KEY
# SILICONFLOW_MODEL 须与模型广场完整 ID 一致，例如 deepseek-ai/DeepSeek-V3

go mod tidy
make run
```

```bash
curl -s http://127.0.0.1:12120/healthz
curl -s http://127.0.0.1:12120/v1/info

# SSE
curl -N 'http://127.0.0.1:12120/v1/chat?query=我想招聘一个AI%20FDE工程师，帮我细化要求并记住'

# JSON
curl -s -X POST http://127.0.0.1:12120/v1/chat \
  -H 'content-type: application/json' \
  -d '{"query":"去简历库搜索符合 AI FDE 的人选，联系他们的助手 bot，最后给出匹配度最高的 3 位"}'

# 画像
curl -s 'http://127.0.0.1:12120/v1/profile?owner_id=user-alice'

# 语音（需配置 ASR）
curl -s -X POST http://127.0.0.1:12120/v1/voice \
  -F file=@./sample.wav \
  -F session_id=ses_demo
```

## 招聘 Demo

1. 招聘方：`make run`（`12120`）
2. 可选真实 A2A：
   - `make peer` → `8081`（对应样例「陈明」）
   - `make peer2` → `8082`（「李华」）
   - `make peer3` → `8083`（「王菲」）
   - `make market` → `8090`（人才招聘市场 Agent，注册到 `12120`）
3. 对招聘 bot：细化 AI FDE 要求 → 搜简历 / 搜 A2A 市场并派任 → 合并结果 →（可选）联系对方 bot → 给出 Top3

说明：样例简历里的 GitHub login 为演示占位，真实平台上可能 404；Agent 会回退到简历内容继续匹配。

## 容器化部署

单镜像双进程：`recruiter`（招聘方，宿主机 `:12120`）与 `talent-market`（人才招聘市场 Agent，宿主机 `:18090`，容器内 `:8090`）。启动后经 `host.docker.internal` 注册到宿主机 [Agent Market](https://github.com/kelvins-io/agent-market)（默认 `:13100`）。

```bash
cp .env.docker.example .env.docker
# 填入 SILICONFLOW_API_KEY；可选改 DOCKER_A2A_REGISTRY_URL / 端口

make docker-up          # 构建并后台启动
# 招聘方：http://127.0.0.1:12120/healthz
# 人才市场：http://127.0.0.1:18090/healthz
# 发现：http://127.0.0.1:13100 （需先 docker compose up agent-market）

make docker-logs
make docker-down
```

| 服务 | 对外端口 | 说明 |
| --- | --- | --- |
| recruiter | 12120 | 招聘方 Bot，托管本机 `/registry`，并向 Agent Market 自注册 |
| talent-market | 18090 | `A2A_MARKET_RECRUIT_MODE=true`，注册到 recruiter + Agent Market |

环境变量见 `.env.docker.example`。镜像默认 `Dockerfile` 的 `from-source` stage（容器内编译）；若需避开容器拉 Go toolchain，可先 `make docker-bin` 再 `docker build --target runtime`。

```bash
curl -s http://127.0.0.1:12120/healthz
curl -s 'http://127.0.0.1:13100/api/v1/agents/bot-recruiter-1' | jq .
```

## 配置要点

| 变量 | 含义 |
| --- | --- |
| `BOT_ID` / `BOT_NAME` / `BOT_PUBLIC_URL` | 本 bot 身份与对外地址 |
| `OWNER_ID` | 默认画像归属人 |
| `HOST` / `PORT` | 监听地址，默认 `127.0.0.1:12120` |
| `A2A_BEARER_TOKEN` | A2A Bearer（兼容 `A2A_SHARED_SECRET`） |
| `A2A_PATH` | JSON-RPC 路径，默认 `/a2a` |
| `A2A_AUTO_REGISTER` | 启动后自动注册，默认 `true` |
| `A2A_HOST_REGISTRY` | 本机挂载 Registry API，默认 `true` |
| `A2A_REGISTRY_URL` | 外部 Registry 根 URL（逗号分隔）；空则注册到 `BOT_PUBLIC_URL` |
| `A2A_REGISTRY_HEARTBEAT_SEC` | 心跳间隔秒，默认 `60` |
| `A2A_MARKET_RECRUIT_MODE` | `true` 时本 bot 作为「人才招聘」市场 Agent |
| `MARKET_LAT` / `MARKET_LNG` | 注册到 Agent Market 时的坐标（可选） |
| `BOT_DOCS_ROOT` / `BOT_RESUME_ROOT` | 授权文档 / 简历目录 |
| `SQLITE_PATH` | 数据库路径，默认 `./data/bot.db` |
| `MODEL_TYPE` | `siliconflow` / `openai` / `ark` |
| `SILICONFLOW_API_KEY` | 硅基流动 API Key |
| `SILICONFLOW_MODEL` | 模型广场完整 ID |
| `SILICONFLOW_BASE_URL` | 默认 `https://api.siliconflow.cn/v1` |
| `GITHUB_TOKEN` | GitHub API Token（可选） |
| `ASR_*` | 语音转写（可选，兼容 `/audio/transcriptions`） |
| `MAX_ITERATIONS` | Agent 最大工具循环次数 |

完整示例见 `.env.example`。

## 目录

```
cmd/bot/                 入口
internal/agent/          Eino ChatModelAgent 装配与指令
internal/memory/         画像 / 招聘业务与去重
internal/store/          GORM 模型、迁移、checkpoint
internal/tools/          Agent 工具集
internal/a2a/            公开 A2A（Agent Card + JSON-RPC）
internal/asr/            语音转写客户端
internal/server/         Hertz HTTP / SSE
internal/config/         环境变量加载
data/docs|resumes/       授权样例库
Dockerfile               多阶段镜像（from-source / runtime）
docker-compose.yml       recruiter + talent-market
.env.docker.example      Compose 环境变量模板
```

## 尚未覆盖

- 飞书 / 企微等 IM 通道
- 多实例 Postgres
- 真实候选人 GitHub 深度画像与自动建档
- A2A streaming / push notification / OAuth2 完整企业鉴权
