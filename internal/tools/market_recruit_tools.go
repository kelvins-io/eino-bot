package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/kelvins-io/eino-bot/internal/a2a"
)

type searchMarketIn struct {
	SkillTag    string `json:"skill_tag,omitempty" jsonschema_description:"技能标签，默认「人才招聘」"`
	Limit       int    `json:"limit,omitempty"`
	RegistryURL string `json:"registry_url,omitempty" jsonschema_description:"可选外部 Registry 根 URL；空则用本机目录 + A2A_REGISTRY_URL"`
	PreferTopN  int    `json:"prefer_top_n,omitempty" jsonschema_description:"返回互动率最高的前 N 个，默认 5"`
}

func newSearchA2AMarketTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("search_a2a_market_agents",
		"在 A2A 市场/Registry 搜索 skill 标签为「人才招聘」（可改）的 Agent，按与其他 Agent 互动率从高到低排序。找人步骤的可选来源。",
		func(ctx context.Context, in *searchMarketIn) (string, error) {
			tag := in.SkillTag
			if tag == "" {
				tag = a2a.DefaultMarketSkillTag
			}
			limit := in.PreferTopN
			if limit <= 0 {
				limit = in.Limit
			}
			if limit <= 0 {
				limit = 5
			}

			seen := map[string]struct{}{}
			var agents []a2a.RegisteredAgent

			add := func(list []a2a.RegisteredAgent) {
				for _, a := range list {
					u := strings.TrimRight(a.URL, "/")
					if u == "" {
						continue
					}
					if _, ok := seen[u]; ok {
						continue
					}
					seen[u] = struct{}{}
					agents = append(agents, a)
				}
			}

			add(deps.A2A.SearchMarketAgents(tag, limit*2))

			bases := []string{}
			if in.RegistryURL != "" {
				bases = append(bases, in.RegistryURL)
			}
			bases = append(bases, deps.Cfg.A2ARegistryURLs...)
			for _, base := range bases {
				list, err := deps.A2A.FetchRemoteRegistryAgents(ctx, base, tag, limit*2)
				if err != nil {
					continue
				}
				add(list)
			}

			// Re-sort merged by interaction.
			for i := 0; i < len(agents); i++ {
				for j := i + 1; j < len(agents); j++ {
					if agents[j].InteractionCount > agents[i].InteractionCount {
						agents[i], agents[j] = agents[j], agents[i]
					}
				}
			}
			if len(agents) > limit {
				agents = agents[:limit]
			}

			summaries := make([]map[string]any, 0, len(agents))
			for _, a := range agents {
				skills := []string{}
				if a.AgentCard != nil {
					for _, sk := range a.AgentCard.Skills {
						skills = append(skills, sk.Name)
						skills = append(skills, sk.Tags...)
					}
				}
				summaries = append(summaries, map[string]any{
					"bot_id":            a.BotID,
					"url":               a.URL,
					"name":              cardName(a),
					"interaction_count": a.InteractionCount,
					"skills":            skills,
					"last_seen_at":      a.LastSeenAt,
				})
			}
			hint := "优先选 interaction_count 最高的 Agent，调用 start_market_recruit_task 发起人才招聘任务"
			if len(summaries) == 0 {
				hint = "未找到带「人才招聘」标签的 Agent。可用 make market 启动市场 Agent，或确认其已注册到 Registry。"
			}
			return mustJSON(map[string]any{
				"skill_tag": tag,
				"agents":    summaries,
				"count":     len(summaries),
				"hint":      hint,
			})
		})
}

func cardName(a a2a.RegisteredAgent) string {
	if a.AgentCard != nil && a.AgentCard.Name != "" {
		return a.AgentCard.Name
	}
	return a.BotID
}

type startMarketTaskIn struct {
	JobID       string `json:"job_id" jsonschema_description:"已保存的招聘需求 ID"`
	AgentURL    string `json:"agent_url,omitempty" jsonschema_description:"市场 Agent 根 URL；空则自动选互动率最高的人才招聘 Agent"`
	AgentBotID  string `json:"agent_bot_id,omitempty"`
	SkillTag    string `json:"skill_tag,omitempty"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema_description:"派任后短暂等待回调的秒数，默认 2"`
}

func newStartMarketRecruitTaskTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("start_market_recruit_task",
		"向 A2A 人才招聘市场 Agent 交换信息并发起招聘任务；对方完成后会回调本服务，也可用 poll_market_recruit_task 定期询问。",
		func(ctx context.Context, in *startMarketTaskIn) (string, error) {
			if in.JobID == "" {
				return "", fmt.Errorf("job_id is required")
			}
			job, err := deps.Memory.GetJob(ctx, in.JobID)
			if err != nil {
				return mustJSON(map[string]any{"ok": false, "error": err.Error()})
			}

			agentURL := strings.TrimRight(in.AgentURL, "/")
			agentBotID := in.AgentBotID
			if agentURL == "" {
				tag := in.SkillTag
				if tag == "" {
					tag = a2a.DefaultMarketSkillTag
				}
				list := deps.A2A.SearchMarketAgents(tag, 5)
				if len(list) == 0 {
					return mustJSON(map[string]any{
						"ok":    false,
						"error": "no market recruit agent found",
						"hint":  "先 search_a2a_market_agents，或 make market 后注册",
					})
				}
				agentURL = list[0].URL
				agentBotID = list[0].BotID
			}

			task, err := deps.Memory.CreateMarketRecruitTask(ctx, in.JobID, agentURL, agentBotID)
			if err != nil {
				return "", err
			}
			callbackURL := strings.TrimRight(deps.Cfg.BotPublicURL, "/") + "/v1/recruit/market/callback"
			_ = deps.Memory.MarkMarketRecruitTaskRunning(ctx, task.ID)

			reply, sendErr := deps.A2A.DelegateRecruitTask(ctx, agentURL, agentBotID, a2a.MarketTaskCreatePayload{
				TaskID: task.ID, JobID: in.JobID, Title: job.Title, Requirements: job.Requirements,
				CallbackURL: callbackURL, CallbackToken: task.CallbackToken,
				FromBotID: deps.Cfg.BotID, FromURL: deps.Cfg.BotPublicURL,
			})
			if sendErr != nil {
				_, _ = deps.Memory.CompleteMarketRecruitTask(ctx, task.ID, nil, sendErr.Error())
				return mustJSON(map[string]any{
					"ok":        false,
					"task_id":   task.ID,
					"agent_url": agentURL,
					"error":     sendErr.Error(),
					"hint":      "市场 Agent 不可达；继续 search_resumes，稍后再 poll 或换 Agent",
					"fallback":  true,
				})
			}

			// Opportunistic short wait for fast local market agents.
			wait := in.WaitSeconds
			if wait <= 0 {
				wait = 2
			}
			if wait > 15 {
				wait = 15
			}
			refreshed, _ := deps.Memory.GetMarketRecruitTask(ctx, task.ID)
			status := "running"
			if refreshed != nil {
				status = refreshed.Status
			}
			return mustJSON(map[string]any{
				"ok":             true,
				"task_id":        task.ID,
				"job_id":         in.JobID,
				"agent_url":      agentURL,
				"agent_bot_id":   agentBotID,
				"status":         status,
				"callback_url":   callbackURL,
				"delegate_reply": reply,
				"hint":           "若 status 非 completed，稍后调用 poll_market_recruit_task；完成后 merge_recruit_findings 与本地简历合并，再 contact_peer_bot",
				"wait_seconds":   wait,
			})
		})
}

type pollMarketTaskIn struct {
	TaskID    string `json:"task_id,omitempty"`
	JobID     string `json:"job_id,omitempty" jsonschema_description:"未给 task_id 时列出该 job 下任务"`
	AskRemote bool   `json:"ask_remote,omitempty" jsonschema_description:"true 时向市场 Agent 发 recruit.task.status"`
}

func newPollMarketRecruitTaskTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("poll_market_recruit_task",
		"查询人才招聘市场任务结果（本地已收回调则直接返回；否则可 ask_remote 向对方询问）。",
		func(ctx context.Context, in *pollMarketTaskIn) (string, error) {
			if in.TaskID == "" {
				list, err := deps.Memory.ListMarketRecruitTasks(ctx, in.JobID)
				if err != nil {
					return "", err
				}
				return mustJSON(map[string]any{"tasks": list, "hint": "对未完成任务带 task_id 再 poll，或 ask_remote=true"})
			}
			task, err := deps.Memory.GetMarketRecruitTask(ctx, in.TaskID)
			if err != nil {
				return mustJSON(map[string]any{"ok": false, "error": err.Error()})
			}
			if task.Status == "completed" || task.Status == "failed" {
				return mustJSON(map[string]any{"ok": true, "task": task, "source": "local_callback_or_store"})
			}
			if in.AskRemote {
				reply, err := deps.A2A.PollRecruitTask(ctx, task.AgentURL, task.AgentBotID, task.ID)
				if err != nil {
					return mustJSON(map[string]any{
						"ok": true, "task": task, "remote_error": err.Error(),
						"hint": "对方暂不可达，可稍后再问或等待回调 /v1/recruit/market/callback",
					})
				}
				// Try extract candidates from reply.
				if cands := extractCandidatesFromA2AReply(reply); len(cands) > 0 {
					updated, _ := deps.Memory.CompleteMarketRecruitTask(ctx, task.ID, cands, "")
					return mustJSON(map[string]any{"ok": true, "task": updated, "source": "remote_poll", "reply": reply})
				}
				return mustJSON(map[string]any{"ok": true, "task": task, "remote_reply": reply, "source": "remote_poll"})
			}
			return mustJSON(map[string]any{
				"ok": true, "task": task,
				"hint": "仍在进行；设 ask_remote=true 主动询问，或等待市场 Agent 回调",
			})
		})
}

type mergeRecruitIn struct {
	JobID      string `json:"job_id"`
	TaskID     string `json:"task_id,omitempty" jsonschema_description:"市场任务 ID；空则用该 job 最近已完成任务"`
	LocalQuery string `json:"local_query,omitempty" jsonschema_description:"本地简历关键词；传 '-' 跳过本地扫描"`
	MaxLocal   int    `json:"max_local,omitempty"`
}

func newMergeRecruitFindingsTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("merge_recruit_findings",
		"将 A2A 市场招聘任务结果与本地简历扫描合并，输出统一候选列表供匹配分析；之后再执行 contact_peer_bot（步骤3）。",
		func(ctx context.Context, in *mergeRecruitIn) (string, error) {
			if in.JobID == "" {
				return "", fmt.Errorf("job_id is required")
			}

			var marketResults []map[string]any
			var taskMeta map[string]any
			if in.TaskID != "" {
				t, err := deps.Memory.GetMarketRecruitTask(ctx, in.TaskID)
				if err == nil {
					marketResults = t.Results
					taskMeta = map[string]any{"task_id": t.ID, "status": t.Status, "agent_url": t.AgentURL}
				}
			} else {
				list, _ := deps.Memory.ListMarketRecruitTasks(ctx, in.JobID)
				for _, t := range list {
					if t.Status == "completed" && len(t.Results) > 0 {
						marketResults = t.Results
						taskMeta = map[string]any{"task_id": t.ID, "status": t.Status, "agent_url": t.AgentURL}
						break
					}
				}
			}

			localHits := []map[string]any{}
			if in.LocalQuery != "-" {
				max := in.MaxLocal
				if max <= 0 {
					max = 20
				}
				localHits = scanResumes(deps.Cfg.ResumeRoot, in.LocalQuery, max)
			}

			merged := mergeCandidateMaps(marketResults, localHits)
			return mustJSON(map[string]any{
				"job_id":       in.JobID,
				"market_task":  taskMeta,
				"market_count": len(marketResults),
				"local_count":  len(localHits),
				"merged":       merged,
				"merged_count": len(merged),
				"hint":         "对 merged 中人选 upsert_candidate 打分，再 contact_peer_bot 联系其助手（步骤3），最后 rank_top_candidates",
			})
		})
}

func extractCandidatesFromA2AReply(reply map[string]any) []map[string]any {
	// reply shape from resultToMap → message.data or nested
	try := []any{reply}
	if msg, ok := reply["message"].(map[string]any); ok {
		try = append(try, msg)
		if d, ok := msg["data"].(map[string]any); ok {
			try = append(try, d)
		}
	}
	for _, node := range try {
		m, ok := node.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := m["candidates"].([]any); ok {
			out := make([]map[string]any, 0, len(c))
			for _, item := range c {
				if mm, ok := item.(map[string]any); ok {
					out = append(out, mm)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func scanResumes(root, query string, maxFiles int) []map[string]any {
	q := strings.ToLower(strings.TrimSpace(query))
	var hits []map[string]any
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if len(hits) >= maxFiles {
			return filepath.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".txt" && ext != ".json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(raw)
		if q != "" && !strings.Contains(strings.ToLower(text), q) {
			return nil
		}
		preview := text
		if len(preview) > 1200 {
			preview = preview[:1200] + "..."
		}
		rel, _ := filepath.Rel(root, path)
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		botURL := ""
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, "http://") || strings.Contains(line, "https://") {
				for _, p := range strings.Fields(line) {
					if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
						botURL = strings.TrimRight(p, "/).,")
						break
					}
				}
			}
		}
		hits = append(hits, map[string]any{
			"name":         name,
			"source":       "resume",
			"path":         rel,
			"preview":      preview,
			"bot_endpoint": botURL,
			"score":        50.0,
		})
		return nil
	})
	return hits
}

func mergeCandidateMaps(market, local []map[string]any) []map[string]any {
	keyOf := func(m map[string]any) string {
		if v, ok := m["github_login"].(string); ok && v != "" {
			return "gh:" + strings.ToLower(v)
		}
		if v, ok := m["bot_endpoint"].(string); ok && v != "" {
			return "bot:" + strings.TrimRight(strings.ToLower(v), "/")
		}
		if v, ok := m["name"].(string); ok {
			return "name:" + strings.ToLower(v)
		}
		if v, ok := m["path"].(string); ok {
			return "path:" + v
		}
		return fmt.Sprintf("%v", m)
	}
	seen := map[string]map[string]any{}
	order := []string{}
	add := func(m map[string]any) {
		k := keyOf(m)
		if prev, ok := seen[k]; ok {
			// Keep higher score; union sources.
			ps, _ := prev["score"].(float64)
			ns, _ := m["score"].(float64)
			if ns > ps {
				m["sources"] = appendSource(prev, m)
				seen[k] = m
			} else {
				prev["sources"] = appendSource(prev, m)
			}
			return
		}
		m["sources"] = []string{fmt.Sprint(m["source"])}
		seen[k] = m
		order = append(order, k)
	}
	for _, m := range market {
		add(cloneMap(m))
	}
	for _, m := range local {
		add(cloneMap(m))
	}
	out := make([]map[string]any, 0, len(order))
	for _, k := range order {
		out = append(out, seen[k])
	}
	return out
}

func appendSource(prev, neu map[string]any) []string {
	set := map[string]struct{}{}
	var out []string
	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := set[s]; ok {
			return
		}
		set[s] = struct{}{}
		out = append(out, s)
	}
	if arr, ok := prev["sources"].([]string); ok {
		for _, s := range arr {
			add(s)
		}
	}
	add(fmt.Sprint(prev["source"]))
	add(fmt.Sprint(neu["source"]))
	return out
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
