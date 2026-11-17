package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"

	"github.com/kelvins-io/eino-bot/internal/a2a"
	"github.com/kelvins-io/eino-bot/internal/config"
	"github.com/kelvins-io/eino-bot/internal/memory"
	"github.com/kelvins-io/eino-bot/internal/store"
	bottools "github.com/kelvins-io/eino-bot/internal/tools"
)

type Runtime struct {
	Cfg     *config.Config
	Memory  *memory.Service
	A2A     *a2a.Service
	DB      *store.DB
	Runner  *adk.Runner
	OwnerID string
}

func NewRuntime(ctx context.Context, cfg *config.Config, db *store.DB, mem *memory.Service, a2aSvc *a2a.Service, ownerID string) (*Runtime, error) {
	if ownerID == "" {
		ownerID = "user-default"
	}
	_ = os.MkdirAll(cfg.DocsRoot, 0o755)
	_ = os.MkdirAll(cfg.ResumeRoot, 0o755)

	cm, err := newChatModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	toolsList, err := bottools.All(&bottools.Deps{
		Cfg: cfg, Memory: mem, A2A: a2aSvc, OwnerID: ownerID,
	})
	if err != nil {
		return nil, err
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        cfg.BotName,
		Description: "Persistent personal agent with memory, docs/github analysis and A2A exchange",
		Instruction: instruction(cfg, ownerID),
		Model:       cm,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		MaxIterations: cfg.MaxIterations,
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: store.NewCheckPointStore(db),
	})
	return &Runtime{
		Cfg: cfg, Memory: mem, A2A: a2aSvc, DB: db, Runner: runner, OwnerID: ownerID,
	}, nil
}

func instruction(cfg *config.Config, ownerID string) string {
	return strings.TrimSpace(fmt.Sprintf(`
你是用户的长期个人助手 Bot（bot_id=%s, name=%s, public_url=%s）。
默认用简体中文回复。当前用户 owner_id=%s。

## 核心职责
1. 接收文字或语音转写文本，提炼原子事实，调用 remember_fact 写入记忆；重复信息会被自动去重。
2. 需要时调用 get_profile / search_memory 回顾画像，并用 update_profile 维护显示名、摘要、bot 联系方式。
3. 用户授权后，用 analyze_docs 分析文档库（%s），用 search_resumes 检索简历库（%s）。
4. 用 search_github / analyze_github_user 在 GitHub 发现人选与技能线索。
5. 用 contact_peer_bot 通过公开 A2A 协议（Agent Card + JSON-RPC SendMessage）与其他助手交换信息（handshake / profile.inquiry / recruit.inquiry）。endpoint 填对方根 URL。
6. 招聘场景闭环：
   1) save_job_requirement 细化并记住要求
   2) 找人（可并行）：search_resumes / search_github；可选 search_a2a_market_agents（skill 标签默认「人才招聘」，优先互动率高的）→ start_market_recruit_task 派任 → 等待回调或 poll_market_recruit_task → merge_recruit_findings 与本地扫描合并
   3) contact_peer_bot 联系人选助手交换信息
   4) upsert_candidate 打分 → rank_top_candidates 标记 Top3

## 行为约束
- 事实尽量短、原子化；不要把整段对话原样塞进记忆。
- 未经用户意图，不主动对外发送敏感隐私；A2A 仅交换对方授权摘要。
- contact_peer_bot 若返回 reachable=false，视为对方离线：继续用简历/GitHub/市场结果打分与排序，不要停止整轮任务。
- 市场 Agent 不可达或任务未完成：先用本地简历继续，并提示稍后 poll_market_recruit_task。
- analyze_github_user / search_github 若返回 found=false 或 ok=false（常见于演示账号 404），继续用简历内容打分，不要停止整轮任务。
- 工具失败时说明原因并给出下一步建议。
- 最终回答要可执行：包含已做动作、关键结果（如 Top3）、待确认项。
`, cfg.BotID, cfg.BotName, cfg.BotPublicURL, ownerID, cfg.DocsRoot, cfg.ResumeRoot))
}

func newChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	if cfg.ModelType == "ark" {
		return ark.NewChatModel(ctx, &ark.ChatModelConfig{
			APIKey:  cfg.ArkAPIKey,
			Model:   cfg.ArkModel,
			BaseURL: cfg.ArkBaseURL,
			Thinking: &arkModel.Thinking{
				Type: arkModel.ThinkingTypeDisabled,
			},
		})
	}
	// siliconflow / openai：均走 OpenAI 兼容 Chat Completions。
	// 硅基流动模型 ID 必须与模型广场一致，例如 deepseek-ai/DeepSeek-V3、Qwen/Qwen2.5-72B-Instruct。
	return openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  cfg.OpenAIAPIKey,
		Model:   cfg.OpenAIModel,
		BaseURL: cfg.OpenAIBaseURL,
		ByAzure: cfg.OpenAIByAzure,
	})
}

// Tools exposes tool list for diagnostics.
func ToolsOf(rt *Runtime) []tool.BaseTool {
	list, _ := bottools.All(&bottools.Deps{
		Cfg: rt.Cfg, Memory: rt.Memory, A2A: rt.A2A, OwnerID: rt.OwnerID,
	})
	return list
}
