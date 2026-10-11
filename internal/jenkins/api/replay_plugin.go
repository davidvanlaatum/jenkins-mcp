package api

import (
	"context"
	"encoding/json"
	"strings"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
)

func (a *API) pluginReplayScripts(ctx context.Context, job string, number int) (string, map[string]string, bool, error) {
	b, available, err := a.client.CallMCPTool(ctx, "getReplayScripts", map[string]any{"jobFullName": job, "buildNumber": number})
	if err != nil || !available {
		return "", nil, available, err
	}
	malformed := func() (string, map[string]string, bool, error) {
		return "", nil, true, apperrors.Wrap(apperrors.CodeJenkins, "invalid Jenkins MCP replay scripts response", nil)
	}
	var raw struct {
		MainScript    *string             `json:"mainScript"`
		LoadedScripts *map[string]*string `json:"loadedScripts"`
	}
	if err := json.Unmarshal(b, &raw); err != nil || raw.MainScript == nil || raw.LoadedScripts == nil {
		return malformed()
	}
	if *raw.MainScript == "" && len(*raw.LoadedScripts) == 0 {
		return "", nil, true, apperrors.Wrap(apperrors.CodePermissionDenied, "Jenkins MCP did not disclose Replay scripts; Item.EXTENDED_READ is required", nil)
	}
	loaded := make(map[string]string, len(*raw.LoadedScripts))
	for name, content := range *raw.LoadedScripts {
		// ReplayAction.doRun reads form keys with dots replaced by underscores.
		// Match native Replay textarea identifiers regardless of retrieval backend.
		id := strings.ReplaceAll(name, ".", "_")
		if strings.TrimSpace(id) == "" || id == "mainScript" || content == nil {
			return malformed()
		}
		if _, collision := loaded[id]; collision {
			return malformed()
		}
		loaded[id] = *content
	}
	return *raw.MainScript, loaded, true, nil
}
