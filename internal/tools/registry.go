package tools

import (
	"github.com/cloudwego/eino/components/tool"

	"github.com/kelvins-io/eino-bot/internal/a2a"
	"github.com/kelvins-io/eino-bot/internal/config"
	"github.com/kelvins-io/eino-bot/internal/memory"
)

type Deps struct {
	Cfg     *config.Config
	Memory  *memory.Service
	A2A     *a2a.Service
	OwnerID string
}

func All(deps *Deps) ([]tool.BaseTool, error) {
	var out []tool.BaseTool
	builders := []func(*Deps) (tool.BaseTool, error){
		newRememberFactTool,
		newGetProfileTool,
		newSearchMemoryTool,
		newUpdateProfileTool,
		newAnalyzeDocsTool,
		newSearchGitHubTool,
		newAnalyzeGitHubUserTool,
		newSaveJobTool,
		newUpdateJobTool,
		newSearchResumesTool,
		newSearchA2AMarketTool,
		newStartMarketRecruitTaskTool,
		newPollMarketRecruitTaskTool,
		newMergeRecruitFindingsTool,
		newUpsertCandidateTool,
		newContactPeerBotTool,
		newRankCandidatesTool,
		newListCandidatesTool,
	}
	for _, b := range builders {
		t, err := b(deps)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
