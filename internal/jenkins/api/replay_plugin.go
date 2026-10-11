package api

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
)

func (a *API) pluginReplayScriptsRaw(ctx context.Context, job string, number int) (string, map[string]string, bool, error) {
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
	ids := make(map[string]bool, len(*raw.LoadedScripts))
	for name, content := range *raw.LoadedScripts {
		// ReplayAction.doRun reads form keys with dots replaced by underscores.
		// Match native Replay textarea identifiers regardless of retrieval backend.
		id := strings.ReplaceAll(name, ".", "_")
		if strings.TrimSpace(id) == "" || id == "mainScript" || content == nil {
			return malformed()
		}
		if _, collision := ids[id]; collision {
			return malformed()
		}
		ids[id] = true
		loaded[name] = *content
	}
	return *raw.MainScript, loaded, true, nil
}

func (a *API) pluginReplayScripts(ctx context.Context, job string, number int) (string, map[string]string, bool, error) {
	main, raw, available, err := a.pluginReplayScriptsRaw(ctx, job, number)
	loaded := make(map[string]string, len(raw))
	for name, content := range raw {
		loaded[strings.ReplaceAll(name, ".", "_")] = content
	}
	return main, loaded, available, err
}

// PluginReplayBuild schedules once via ReplayAction and returns its actual queue
// ID. available=false is the only signal permitting native scheduling fallback.
func (a *API) PluginReplayBuild(ctx context.Context, job string, number int, mainOverride *string, overrides map[string]string) (int64, []string, bool, error) {
	var included []string
	prepared := false
	b, available, err := a.client.CallMCPToolPrepared(ctx, "replayBuild", func() (any, error) {
		main, loaded, originalsAvailable, err := a.pluginReplayScriptsRaw(ctx, job, number)
		if err != nil {
			return nil, err
		}
		if !originalsAvailable {
			return nil, apperrors.Wrap(apperrors.CodeJenkins, "Jenkins MCP replayBuild requires getReplayScripts to preserve original scripts", nil)
		}
		if mainOverride != nil {
			main = *mainOverride
		}
		names := make(map[string]string, len(loaded))
		for name := range loaded {
			id := strings.ReplaceAll(name, ".", "_")
			names[id] = name
			included = append(included, id)
		}
		for id, content := range overrides {
			name, ok := names[id]
			if !ok {
				return nil, apperrors.Wrap(apperrors.CodeInvalidRequest, "unknown loaded replay script identifier", map[string]any{"id": id})
			}
			loaded[name] = content
		}
		sort.Strings(included)
		included = append([]string{"main"}, included...)
		prepared = true
		return map[string]any{"jobFullName": job, "buildNumber": number, "mainScript": main, "loadedScripts": loaded, "tree": "id"}, nil
	})
	if err != nil || !available {
		if err != nil && prepared {
			code := apperrors.CodeJenkins
			var original *apperrors.Error
			if errors.As(err, &original) {
				code = original.Code
			}
			err = apperrors.Wrap(code, "Jenkins MCP Replay scheduling failed; outcome may be uncertain, inspect Jenkins before retrying", map[string]any{"tool": "replayBuild", "schedulingOutcome": "uncertain"})
		}
		return 0, nil, available, err
	}
	var queue struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(b, &queue) != nil || queue.ID <= 0 {
		return 0, nil, true, apperrors.Wrap(apperrors.CodeJenkins, "invalid Jenkins MCP replay queue response; scheduling outcome is uncertain, do not retry automatically", map[string]any{"tool": "replayBuild", "schedulingOutcome": "uncertain"})
	}
	return queue.ID, included, true, nil
}
