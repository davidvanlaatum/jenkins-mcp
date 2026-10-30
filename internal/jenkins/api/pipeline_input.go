package api

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/jenkins/urlx"
)

// PipelineInputs reads exported InputActions without requiring the wfapi plugin.
// Defaults are deliberately excluded: even ordinary parameter names may hold secrets.
func (a *API) PipelineInputs(ctx context.Context, job string, build int) ([]model.PipelineInput, error) {
	var raw struct {
		Actions []struct {
			Executions []struct {
				ID    string `json:"id"`
				Input *struct {
					Message    string `json:"message"`
					OK         string `json:"ok"`
					Submitter  string `json:"submitter"`
					Parameters []struct {
						Class       string   `json:"_class"`
						Name        string   `json:"name"`
						Description string   `json:"description"`
						Choices     []string `json:"choices"`
					} `json:"parameters"`
				} `json:"input"`
			} `json:"executions"`
		} `json:"actions"`
	}
	q := url.Values{"tree": {"actions[executions[id,input[message,ok,submitter,parameters[name,description,choices]]]]"}}
	if err := a.client.GetJSON(ctx, urlx.JobPath(job)+"/"+strconv.Itoa(build)+"/api/json", q, &raw); err != nil {
		return nil, err
	}
	inputs := make([]model.PipelineInput, 0)
	for _, action := range raw.Actions {
		for _, execution := range action.Executions {
			if execution.Input == nil || execution.ID == "" || execution.Input.Parameters == nil {
				return nil, apperrors.New(apperrors.CodeJenkins, "Jenkins returned incomplete Pipeline input metadata")
			}
			input := model.PipelineInput{ID: execution.ID, Message: execution.Input.Message, OK: execution.Input.OK, Submitter: execution.Input.Submitter, Parameters: make([]model.PipelineInputParameter, 0)}
			for _, p := range execution.Input.Parameters {
				kind := pipelineParameterType(p.Class)
				input.Parameters = append(input.Parameters, model.PipelineInputParameter{Name: p.Name, Description: p.Description, Type: kind, Choices: p.Choices, Supported: kind != "unsupported"})
			}
			inputs = append(inputs, input)
		}
	}
	return inputs, nil
}

func pipelineParameterType(class string) string {
	switch class {
	case "hudson.model.StringParameterDefinition":
		return "string"
	case "hudson.model.TextParameterDefinition":
		return "text"
	case "hudson.model.BooleanParameterDefinition":
		return "boolean"
	case "hudson.model.ChoiceParameterDefinition":
		return "choice"
	case "hudson.model.PasswordParameterDefinition":
		return "password"
	default:
		return "unsupported"
	}
}

func ValidatePipelineInputID(id string) error {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\%?#\x00\r\n") {
		return apperrors.New(apperrors.CodeInvalidRequest, "inputId must be a non-empty Jenkins input identifier without path separators or URL escapes")
	}
	return nil
}

func (a *API) RespondPipelineInput(ctx context.Context, job string, build int, id string, abort bool, parameters map[string]any) (model.PipelineInputResult, error) {
	if err := ValidatePipelineInputID(id); err != nil {
		return model.PipelineInputResult{}, err
	}
	inputs, err := a.PipelineInputs(ctx, job, build)
	if err != nil {
		return model.PipelineInputResult{}, err
	}
	var selected *model.PipelineInput
	for i := range inputs {
		if inputs[i].ID == id {
			selected = &inputs[i]
			break
		}
	}
	if selected == nil {
		return model.PipelineInputResult{}, apperrors.New(apperrors.CodeNotFound, "Pipeline input is no longer pending or does not exist")
	}
	endpoint := "abort"
	var form url.Values
	if !abort {
		values, err := pipelineInputValues(*selected, parameters)
		if err != nil {
			return model.PipelineInputResult{}, err
		}
		endpoint = "proceedEmpty"
		if len(values) > 0 {
			endpoint = "proceed"
			body, err := json.Marshal(map[string]any{"parameter": values})
			if err != nil {
				return model.PipelineInputResult{}, apperrors.New(apperrors.CodeInvalidRequest, "invalid Pipeline input values")
			}
			form = url.Values{"json": {string(body)}}
		}
	}
	path := urlx.JobPath(job) + "/" + strconv.Itoa(build) + "/input/" + url.PathEscape(id) + "/" + endpoint
	status, _, _, err := a.client.Post(ctx, path, nil, form)
	if err != nil {
		return model.PipelineInputResult{}, err
	}
	if status < 200 || status > 299 {
		return model.PipelineInputResult{}, jenkinsHTTPError(status)
	}
	return model.PipelineInputResult{InputID: id, Accepted: true}, nil
}

func pipelineInputValues(input model.PipelineInput, parameters map[string]any) ([]map[string]any, error) {
	invalid := func(message string) ([]map[string]any, error) {
		return nil, apperrors.New(apperrors.CodeInvalidRequest, message)
	}
	if len(parameters) != len(input.Parameters) {
		return invalid("provide exactly the parameters listed by jenkins_get_pipeline_inputs; defaults are not applied")
	}
	values := make([]map[string]any, 0, len(parameters))
	for _, p := range input.Parameters {
		if !p.Supported {
			return nil, apperrors.New(apperrors.CodeUnsupported, "Pipeline input contains an unsupported parameter type; use Jenkins UI")
		}
		value, ok := parameters[p.Name]
		if !ok {
			return invalid("missing Pipeline input parameter")
		}
		if p.Type == "boolean" {
			if _, ok := value.(bool); !ok {
				return invalid("boolean Pipeline input parameters require JSON booleans")
			}
		} else {
			s, ok := value.(string)
			if !ok {
				return invalid("text Pipeline input parameters require JSON strings")
			}
			if p.Type == "choice" {

				if !slices.Contains(p.Choices, s) {
					return invalid("Pipeline input choice is not one of the available choices")
				}
			}
		}
		values = append(values, map[string]any{"name": p.Name, "value": value})
	}
	return values, nil
}
