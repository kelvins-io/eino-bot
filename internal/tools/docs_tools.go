package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type analyzeDocsIn struct {
	OwnerID string `json:"owner_id,omitempty"`
	RelPath string `json:"rel_path,omitempty" jsonschema_description:"相对 BOT_DOCS_ROOT 的路径，空则扫描整个授权目录"`
	MaxFiles int   `json:"max_files,omitempty"`
}

func newAnalyzeDocsTool(deps *Deps) (tool.BaseTool, error) {
	return utils.InferTool("analyze_docs",
		"分析用户授权文档库，抽取可写入画像的事实线索（文本摘要片段）。",
		func(ctx context.Context, in *analyzeDocsIn) (string, error) {
			owner := in.OwnerID
			if owner == "" {
				owner = deps.OwnerID
			}
			root := deps.Cfg.DocsRoot
			target := root
			if in.RelPath != "" {
				target = filepath.Join(root, in.RelPath)
			}
			clean, err := safePath(root, target)
			if err != nil {
				return "", err
			}
			maxFiles := in.MaxFiles
			if maxFiles <= 0 {
				maxFiles = 20
			}
			var files []map[string]any
			err = filepath.WalkDir(clean, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				if len(files) >= maxFiles {
					return filepath.SkipAll
				}
				ext := strings.ToLower(filepath.Ext(path))
				if ext != ".md" && ext != ".txt" && ext != ".json" && ext != ".csv" {
					return nil
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				text := string(raw)
				if len(text) > 4000 {
					text = text[:4000] + "..."
				}
				rel, _ := filepath.Rel(root, path)
				// Persist a high-level source pointer; model decides finer facts via remember_fact.
				_, _ = deps.Memory.UpsertFact(ctx, owner, "docs_source", "已授权文档: "+rel, "docs", 0.7)
				files = append(files, map[string]any{
					"path":    rel,
					"bytes":   len(raw),
					"preview": text,
				})
				return nil
			})
			if err != nil && err != filepath.SkipAll {
				return "", err
			}
			return mustJSON(map[string]any{
				"docs_root": root,
				"files":     files,
				"hint":      "请根据 preview 调用 remember_fact 写入去重后的原子事实，并更新 summary",
			})
		})
}

func safePath(root, target string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes authorized docs root")
	}
	return absTarget, nil
}
