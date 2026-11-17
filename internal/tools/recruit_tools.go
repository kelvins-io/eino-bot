package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/kelvins-io/eino-bot/internal/memory"
)

type saveJobIn struct {
	OwnerID      string         `json:"owner_id,omitempty"`
	Title        string         `json:"title" jsonschema_description:"职位名称，如 AI FDE 工程师"`
	Requirements map[string]any `json:"requirements" jsonschema_description:"结构化要求：skills/years/location/must_have/nice_to_have 等"`
}

func newSaveJobTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("save_job_requirement",
		"保存或细化招聘需求到持久存储。",
		func(ctx context.Context, in *saveJobIn) (string, error) {
			owner := in.OwnerID
			if owner == "" {
				owner = deps.OwnerID
			}
			job, err := deps.Memory.SaveJob(ctx, owner, in.Title, in.Requirements)
			if err != nil {
				return "", err
			}
			_, _ = deps.Memory.UpsertFact(ctx, owner, "requirement",
				fmt.Sprintf("招聘需求[%s]: %s", job.ID, in.Title), "chat", 1)
			return mustJSON(job)
		})
}

type updateJobIn struct {
	JobID        string         `json:"job_id"`
	Requirements map[string]any `json:"requirements,omitempty"`
	Status       string         `json:"status,omitempty"`
}

func newUpdateJobTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("update_job_requirement",
		"更新已有招聘需求的细则或状态。",
		func(ctx context.Context, in *updateJobIn) (string, error) {
			job, err := deps.Memory.UpdateJob(ctx, in.JobID, in.Requirements, in.Status)
			if err != nil {
				return "", err
			}
			return mustJSON(job)
		})
}

type searchResumesIn struct {
	JobID   string `json:"job_id,omitempty"`
	Query   string `json:"query,omitempty" jsonschema_description:"简历库关键词过滤"`
	MaxFiles int   `json:"max_files,omitempty"`
}

func newSearchResumesTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("search_resumes",
		"在授权简历库中检索文本简历，返回预览供匹配打分。",
		func(ctx context.Context, in *searchResumesIn) (string, error) {
			root := deps.Cfg.ResumeRoot
			maxFiles := in.MaxFiles
			if maxFiles <= 0 {
				maxFiles = 30
			}
			q := strings.ToLower(strings.TrimSpace(in.Query))
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
				if len(preview) > 2500 {
					preview = preview[:2500] + "..."
				}
				rel, _ := filepath.Rel(root, path)
				hits = append(hits, map[string]any{
					"path":    rel,
					"preview": preview,
					"job_id":  in.JobID,
				})
				return nil
			})
			return mustJSON(map[string]any{
				"resume_root": root,
				"hits":        hits,
				"hint":        "对合适人选调用 upsert_candidate，必要时用 contact_peer_bot 交换信息",
			})
		})
}

type upsertCandidateIn struct {
	JobID       string         `json:"job_id"`
	Name        string         `json:"name"`
	Source      string         `json:"source" jsonschema_description:"resume/github/a2a/manual"`
	GitHubLogin string         `json:"github_login,omitempty"`
	BotEndpoint string         `json:"bot_endpoint,omitempty" jsonschema_description:"对方助手 bot 的根 URL"`
	Profile     map[string]any `json:"profile,omitempty"`
	Score       float64        `json:"score" jsonschema_description:"0-100 匹配分"`
	Notes       string         `json:"notes,omitempty"`
	CandidateID string         `json:"candidate_id,omitempty"`
}

func newUpsertCandidateTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("upsert_candidate",
		"写入或更新候选人记录及匹配分。",
		func(ctx context.Context, in *upsertCandidateIn) (string, error) {
			c := &memory.Candidate{
				ID: in.CandidateID, JobID: in.JobID, Name: in.Name, Source: in.Source,
				GitHubLogin: in.GitHubLogin, BotEndpoint: in.BotEndpoint,
				Profile: in.Profile, Score: in.Score, Notes: in.Notes,
			}
			out, err := deps.Memory.UpsertCandidate(ctx, c)
			if err != nil {
				return "", err
			}
			return mustJSON(out)
		})
}

type rankIn struct {
	JobID string `json:"job_id"`
	TopN  int    `json:"top_n,omitempty"`
}

func newRankCandidatesTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("rank_top_candidates",
		"按匹配分标记并返回 Top N 候选人（默认 3）。",
		func(ctx context.Context, in *rankIn) (string, error) {
			list, err := deps.Memory.RankTopCandidates(ctx, in.JobID, in.TopN)
			if err != nil {
				return "", err
			}
			return mustJSON(map[string]any{
				"job_id":     in.JobID,
				"top_n":      len(list),
				"candidates": list,
			})
		})
}

type listCandidatesIn struct {
	JobID string `json:"job_id"`
}

func newListCandidatesTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("list_candidates",
		"列出某招聘需求下全部候选人。",
		func(ctx context.Context, in *listCandidatesIn) (string, error) {
			list, err := deps.Memory.ListCandidates(ctx, in.JobID)
			if err != nil {
				return "", err
			}
			return mustJSON(list)
		})
}

type contactPeerIn struct {
	Endpoint  string         `json:"endpoint" jsonschema_description:"对方 bot 根 URL，如 http://127.0.0.1:8081"`
	ToBotID   string         `json:"to_bot_id" jsonschema_description:"对方 bot_id"`
	Intent    string         `json:"intent" jsonschema_description:"handshake/profile.inquiry/recruit.inquiry"`
	Payload   map[string]any `json:"payload,omitempty"`
	JobID     string         `json:"job_id,omitempty"`
	Candidate string         `json:"candidate_name,omitempty"`
}

func newContactPeerBotTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("contact_peer_bot",
		"与其他用户的助手 bot 进行 A2A 信息交换（签名校验）。对方离线时返回 reachable=false，不要中断整轮招聘流程。",
		func(ctx context.Context, in *contactPeerIn) (string, error) {
			payload := in.Payload
			if payload == nil {
				payload = map[string]any{}
			}
			if in.JobID != "" {
				payload["job_id"] = in.JobID
			}
			if in.Candidate != "" {
				payload["about_candidate"] = in.Candidate
			}
			reply, err := deps.A2A.Send(ctx, in.Endpoint, in.ToBotID, in.Intent, payload)
			if err != nil {
				// Soft-fail：让 Agent 继续用简历/GitHub 信息打分，而不是整轮 NodeRunError。
				return mustJSON(map[string]any{
					"reachable":     false,
					"endpoint":      in.Endpoint,
					"to_bot_id":     in.ToBotID,
					"intent":        in.Intent,
					"error":         err.Error(),
					"hint":          "对方助手 bot 未启动或不可达。可先基于简历内容 upsert_candidate 打分，并提示用户 make peer 启动联调实例。",
					"fallback_used": true,
				})
			}
			return mustJSON(map[string]any{
				"reachable": true,
				"reply":     reply,
			})
		})
}
