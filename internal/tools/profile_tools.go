package tools

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type rememberFactIn struct {
	OwnerID    string  `json:"owner_id,omitempty" jsonschema_description:"画像归属人 ID，默认当前用户"`
	Category   string  `json:"category" jsonschema_description:"类别：skill/experience/preference/requirement/contact/education/project/general"`
	Content    string  `json:"content" jsonschema_description:"要记住的一条原子事实，尽量短句"`
	Source     string  `json:"source,omitempty" jsonschema_description:"来源：chat/docs/github/a2a"`
	Confidence float64 `json:"confidence,omitempty" jsonschema_description:"置信度 0-1"`
}

func newRememberFactTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("remember_fact",
		"将一条人物事实写入持久记忆；自动去重，重复内容会被跳过。",
		func(ctx context.Context, in *rememberFactIn) (string, error) {
			owner := in.OwnerID
			if owner == "" {
				owner = deps.OwnerID
			}
			res, err := deps.Memory.UpsertFact(ctx, owner, in.Category, in.Content, in.Source, in.Confidence)
			if err != nil {
				return "", err
			}
			_, _ = deps.Memory.RebuildSummary(ctx, owner)
			return mustJSON(res)
		})
}

type ownerIn struct {
	OwnerID string `json:"owner_id,omitempty" jsonschema_description:"画像归属人 ID，默认当前用户"`
}

func newGetProfileTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("get_profile",
		"读取完整人物画像（摘要 + 去重后的事实列表）。",
		func(ctx context.Context, in *ownerIn) (string, error) {
			owner := in.OwnerID
			if owner == "" {
				owner = deps.OwnerID
			}
			p, err := deps.Memory.GetProfile(ctx, owner)
			if err != nil {
				return "", err
			}
			return mustJSON(p)
		})
}

type searchMemoryIn struct {
	OwnerID string `json:"owner_id,omitempty"`
	Query   string `json:"query" jsonschema_description:"关键词"`
	Limit   int    `json:"limit,omitempty"`
}

func newSearchMemoryTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("search_memory",
		"在人物记忆中按关键词检索事实。",
		func(ctx context.Context, in *searchMemoryIn) (string, error) {
			owner := in.OwnerID
			if owner == "" {
				owner = deps.OwnerID
			}
			facts, err := deps.Memory.SearchFacts(ctx, owner, in.Query, in.Limit)
			if err != nil {
				return "", err
			}
			return mustJSON(facts)
		})
}

type updateProfileIn struct {
	OwnerID     string            `json:"owner_id,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	BotContact  string            `json:"bot_contact,omitempty" jsonschema_description:"该用户助手 bot 的公开联系地址"`
	Meta        map[string]string `json:"meta,omitempty"`
}

func newUpdateProfileTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("update_profile",
		"更新画像元信息：显示名、摘要、bot 联系方式等。",
		func(ctx context.Context, in *updateProfileIn) (string, error) {
			owner := in.OwnerID
			if owner == "" {
				owner = deps.OwnerID
			}
			if err := deps.Memory.UpdateProfileMeta(ctx, owner, in.DisplayName, in.Summary, in.BotContact, in.Meta); err != nil {
				return "", err
			}
			p, err := deps.Memory.GetProfile(ctx, owner)
			if err != nil {
				return "", err
			}
			return mustJSON(p)
		})
}

func mustJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
