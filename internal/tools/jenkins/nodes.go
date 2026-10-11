package jenkins

import (
	"context"
	"encoding/json"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
)

type ListNodesResponse struct {
	Nodes []model.Node `json:"nodes" jsonschema:"Jenkins computer summaries grouped with regular and one-off executors; zero-executor configured nodes may have no Computer and be absent"`
}

func ListNodes(ctx context.Context, deps Deps, in BaseRequest) (ListNodesResponse, error) {
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListNodesResponse{}, err
	}
	nodes, err := api.ListNodes(ctx)
	if err != nil {
		return ListNodesResponse{}, err
	}
	for i := range nodes {
		for _, executors := range [][]model.Executor{nodes[i].Executors, nodes[i].OneOffExecutors} {
			for j := range executors {
				executable := executors[j].CurrentExecutable
				if executable == nil {
					continue
				}
				executable.Build = nil
				if ref, err := resolveBuildURL(deps.Config, executable.URL); err == nil {
					executable.Build = &ref
				}
			}
		}
	}
	response := ListNodesResponse{Nodes: nodes}
	encoded, err := json.Marshal(response)
	if err != nil {
		return ListNodesResponse{}, apperrors.Wrap(apperrors.CodeJenkins, "cannot encode Jenkins computer state", err.Error())
	}
	maxBytes := deps.Config.Limits.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = config.Defaults().Limits.MaxResponseBytes
	}
	if int64(len(encoded)) > maxBytes {
		return ListNodesResponse{}, apperrors.Wrap(apperrors.CodeJenkins, "Jenkins computer state exceeds configured response limit", map[string]any{"maxBytes": maxBytes, "bytes": len(encoded)})
	}
	return response, nil
}
