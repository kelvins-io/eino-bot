# eino-bot

基于 [cloudwego/eino](https://github.com/cloudwego/eino) ADK 的可持久化个人 Agent Bot。

支持：

- 24 小时常驻 HTTP 服务（文字 SSE / JSON、语音转写）
- 人物画像记忆 + 内容哈希自动去重（SQLite）
- 授权文档库 / 简历库分析
- GitHub 用户与仓库搜索归纳
- Bot 间 A2A 签名消息交换
- 招聘场景：细化需求 → 搜人 → 联系对方助手 → 标记 Top3

## 架构

```
用户文字/语音 ──► Hertz API ──► Eino ChatModelAgent (ReAct)
                                  │
                                  ├─ remember_fact / get_profile（去重记忆）
                                  ├─ analyze_docs / search_resumes
                                  ├─ search_github / analyze_github_user
                                  ├─ save_job_requirement / upsert_candidate / rank_top_candidates
                                  └─ contact_peer_bot ──► 其他 Bot /v1/a2a/message
```

## 快速开始

```bash
cp .env.example .env
# 编辑 .env，填入 SILICONFLOW_API_KEY
# 模型 ID 从硅基流动模型广场复制，例如 deepseek-ai/DeepSeek-V3

go mod tidy
make run
```

健康检查：

```bash
curl -s http://127.0.0.1:12120/healthz
curl -s http://127.0.0.1:12120/v1/info
```

对话（SSE）：

```bash
curl -N 'http://127.0.0.1:12120/v1/chat?query=我想招聘一个AI%20FDE工程师，帮我细化要求并记住'
```

对话（JSON）：

```bash
curl -s -X POST http://127.0.0.1:12120/v1/chat \
  -H 'content-type: application/json' \
  -d '{"query":"去简历库搜索符合 AI FDE 的人选，联系他们的助手 bot，最后给出匹配度最高的 3 位"}'
```

语音（OpenAI 兼容 ASR）：

```bash
curl -s -X POST http://127.0.0.1:12120/v1/voice \
  -F file=@./sample.wav \
  -F session_id=ses_demo
```

画像：

```bash
curl -s 'http://127.0.0.1:12120/v1/profile?owner_id=user-alice'
```

## 招聘 Demo 流程

1. 启动招聘方 bot：`make run`（12120）
2. 可选：启动候选人 bot：`make peer`（8081，需同样配置模型 key）
3. 对招聘 bot 说：细化 AI FDE 要求并记忆
4. 再要求：搜索 `data/resumes`，必要时 `search_github`，对有 bot 地址的人选 `contact_peer_bot`
5. 调用排序后返回 Top3

样例简历已放在 `data/resumes/`。

## A2A 协议

双方共享 `A2A_SHARED_SECRET`。

- `GET /v1/a2a/discover`：发现能力
- `POST /v1/a2a/message`：携带 HMAC 签名信封

信封字段：`id/from_bot_id/to_bot_id/intent/payload/timestamp/signature`

常用 intent：`handshake`、`profile.inquiry`、`recruit.inquiry`

## 配置要点

| 变量 | 含义 |
| --- | --- |
| `BOT_ID` / `BOT_PUBLIC_URL` | 本 bot 身份与对外地址 |
| `A2A_SHARED_SECRET` | 信任域共享密钥 |
| `BOT_DOCS_ROOT` / `BOT_RESUME_ROOT` | 授权文档/简历目录 |
| `GITHUB_TOKEN` | GitHub API 配额 |
| `MODEL_TYPE` | 默认 `siliconflow`；也可 `openai` / `ark` |
| `SILICONFLOW_API_KEY` | 硅基流动 API Key |
| `SILICONFLOW_MODEL` | 模型广场完整 ID（如 `deepseek-ai/DeepSeek-V3`） |
| `SILICONFLOW_BASE_URL` | 默认 `https://api.siliconflow.cn/v1` |

Agent 能力依赖 Function Calling，请在模型广场选择支持工具调用的对话模型。

## 持久化

SQLite + GORM（默认 `./data/bot.db`，纯 Go 驱动 `glebarez/sqlite`）保存：

- 画像事实（`owner_id + content_hash` 唯一，重复信息自动跳过）
- 对话日志、ADK checkpoint
- 招聘需求、候选人、A2A 报文

进程重启后记忆与会话 checkpoint 仍可用。

## 目录

```
cmd/bot/                 入口
internal/agent/          Eino ChatModelAgent 装配
internal/memory/         画像/招聘持久化与去重
internal/tools/          Agent 工具集
internal/a2a/            Bot 间协议
internal/asr/            语音转写
internal/server/         Hertz HTTP/SSE
data/docs|resumes/       授权样例库
```
