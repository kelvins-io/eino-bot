package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type searchGitHubIn struct {
	Query string `json:"query" jsonschema_description:"GitHub 搜索词，如 language:Go AI agent location:China"`
	Type  string `json:"type,omitempty" jsonschema_description:"users 或 repositories，默认 users"`
	PerPage int  `json:"per_page,omitempty"`
}

func newSearchGitHubTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("search_github",
		"在 GitHub 上搜索用户或仓库，用于招聘/画像线索发现。失败时返回 error 字段，不中断整轮。",
		func(ctx context.Context, in *searchGitHubIn) (string, error) {
			typ := in.Type
			if typ == "" {
				typ = "users"
			}
			perPage := in.PerPage
			if perPage <= 0 || perPage > 30 {
				perPage = 10
			}
			endpoint := fmt.Sprintf("https://api.github.com/search/%s?q=%s&per_page=%d",
				typ, url.QueryEscape(in.Query), perPage)
			raw, err := githubGET(ctx, deps.Cfg.GitHubToken, endpoint)
			if err != nil {
				return mustJSON(map[string]any{
					"ok":      false,
					"query":   in.Query,
					"type":    typ,
					"error":   err.Error(),
					"hint":    "搜索失败时可继续用简历库匹配；若频繁 401/403 请配置 GITHUB_TOKEN",
				})
			}
			var parsed map[string]any
			_ = json.Unmarshal(raw, &parsed)
			parsed["ok"] = true
			return mustJSON(parsed)
		})
}

type analyzeGitHubUserIn struct {
	OwnerID string `json:"owner_id,omitempty" jsonschema_description:"若要把结果写入本地画像，填归属人 ID"`
	Login   string `json:"login" jsonschema_description:"GitHub login"`
}

func newAnalyzeGitHubUserTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("analyze_github_user",
		"读取 GitHub 用户公开资料与最近仓库，归纳技能线索；可选写入本地画像。用户不存在(404)时返回 found=false，不中断整轮。",
		func(ctx context.Context, in *analyzeGitHubUserIn) (string, error) {
			login := strings.TrimSpace(in.Login)
			if login == "" {
				return mustJSON(map[string]any{
					"found": false,
					"error": "login is required",
					"hint":  "请提供真实 GitHub login；样例简历中的账号可能是演示占位",
				})
			}
			userRaw, err := githubGET(ctx, deps.Cfg.GitHubToken, "https://api.github.com/users/"+url.PathEscape(login))
			if err != nil {
				return mustJSON(map[string]any{
					"found":   false,
					"login":   login,
					"error":   err.Error(),
					"hint":    "GitHub 用户不存在或不可访问。继续用简历内容打分即可，勿中断招聘流程。",
				})
			}
			reposRaw, err := githubGET(ctx, deps.Cfg.GitHubToken,
				fmt.Sprintf("https://api.github.com/users/%s/repos?sort=updated&per_page=10", url.PathEscape(login)))
			if err != nil {
				return mustJSON(map[string]any{
					"found":   true,
					"login":   login,
					"warning": "user ok but repos fetch failed: " + err.Error(),
					"user":    json.RawMessage(userRaw),
				})
			}
			var user map[string]any
			var repos []map[string]any
			_ = json.Unmarshal(userRaw, &user)
			_ = json.Unmarshal(reposRaw, &repos)

			langs := map[string]int{}
			var repoNames []string
			for _, r := range repos {
				if name, _ := r["name"].(string); name != "" {
					repoNames = append(repoNames, name)
				}
				if lang, _ := r["language"].(string); lang != "" {
					langs[lang]++
				}
			}
			bio, _ := user["bio"].(string)
			name, _ := user["name"].(string)
			blog, _ := user["blog"].(string)
			result := map[string]any{
				"found":        true,
				"login":        login,
				"name":         name,
				"bio":          bio,
				"blog":         blog,
				"languages":    langs,
				"recent_repos": repoNames,
				"html_url":     user["html_url"],
			}

			owner := in.OwnerID
			if owner != "" {
				if bio != "" {
					_, _ = deps.Memory.UpsertFact(ctx, owner, "experience", "GitHub bio: "+bio, "github", 0.8)
				}
				for lang, n := range langs {
					_, _ = deps.Memory.UpsertFact(ctx, owner, "skill",
						fmt.Sprintf("GitHub 常用语言 %s（近仓出现 %d 次）", lang, n), "github", 0.75)
				}
				if blog != "" && (strings.Contains(blog, "http") || strings.Contains(blog, "agent-card") || strings.Contains(blog, "/a2a")) {
					_ = deps.Memory.UpdateProfileMeta(ctx, owner, name, "", blog, map[string]string{"github": login})
				} else if name != "" {
					_ = deps.Memory.UpdateProfileMeta(ctx, owner, name, "", "", map[string]string{"github": login})
				}
				_, _ = deps.Memory.RebuildSummary(ctx, owner)
			}
			return mustJSON(result)
		})
}

func githubGET(ctx context.Context, token, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "eino-bot")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}
