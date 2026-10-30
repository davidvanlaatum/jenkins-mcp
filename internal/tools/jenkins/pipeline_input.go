package jenkins

import (
	"context"
	"strconv"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
)

type PipelineInputsResponse struct {
	Inputs []model.PipelineInput `json:"inputs" jsonschema:"Pending Pipeline input steps with parameter definitions; no parameter defaults are returned"`
}
type PipelineInputRequest struct {
	Controller string `json:"controller,omitempty" jsonschema:"Jenkins controller id; defaults to configured default controller"`
	Job        string `json:"job" jsonschema:"Jenkins job path, using / for folders"`
	Build      int    `json:"build" jsonschema:"Build number paused for input"`
	InputID    string `json:"inputId" jsonschema:"Exact pending input identifier returned by jenkins_get_pipeline_inputs"`
}
type ProceedPipelineInputRequest struct {
	PipelineInputRequest
	Parameters map[string]any `json:"parameters,omitempty" jsonschema:"All input parameters keyed by exact name: JSON booleans for boolean parameters and strings for string, text, choice, or password parameters; omit for approval-only steps"`
}

func PipelineInputs(ctx context.Context, deps Deps, in BuildRequest) (PipelineInputsResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return PipelineInputsResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, err.Error(), nil)
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return PipelineInputsResponse{}, err
	}
	inputs, err := api.PipelineInputs(ctx, in.Job, in.Build)
	return PipelineInputsResponse{Inputs: inputs}, err
}
func ProceedPipelineInput(ctx context.Context, deps Deps, in ProceedPipelineInputRequest) (model.PipelineInputResult, error) {
	return respondPipelineInput(ctx, deps, in.PipelineInputRequest, false, in.Parameters)
}
func AbortPipelineInput(ctx context.Context, deps Deps, in PipelineInputRequest) (model.PipelineInputResult, error) {
	return respondPipelineInput(ctx, deps, in, true, nil)
}
func respondPipelineInput(ctx context.Context, deps Deps, in PipelineInputRequest, abort bool, parameters map[string]any) (model.PipelineInputResult, error) {
	if !deps.Config.Mutations.Enabled {
		return model.PipelineInputResult{}, apperrors.New(apperrors.CodeMutationDisabled, "mutating Jenkins tools are disabled")
	}
	if err := validateBuild(in.Job, in.Build); err != nil {
		return model.PipelineInputResult{}, apperrors.Wrap(apperrors.CodeInvalidRequest, err.Error(), nil)
	}
	if err := jenkinsapi.ValidatePipelineInputID(in.InputID); err != nil {
		return model.PipelineInputResult{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return model.PipelineInputResult{}, err
	}
	result, err := api.RespondPipelineInput(ctx, in.Job, in.Build, in.InputID, abort, parameters)
	action := "proceed_pipeline_input"
	if abort {
		action = "abort_pipeline_input"
	}
	emit(deps, in.Controller, action, in.Job+"#"+strconv.Itoa(in.Build)+"/input/"+in.InputID, err)
	return result, err
}
