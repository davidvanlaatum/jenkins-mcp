package api

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/jenkins/urlx"
)

const lockableResourceTree = "resources[name,description,labels,labelsAsList,free,reserved,locked,lockCause,lockReason,buildName,note,ephemeral,stolen,reservedBy,reservedTimestamp,properties[name,value]]"

func (a *API) ListLockableResources(ctx context.Context) ([]model.LockableResource, []string, error) {
	var raw struct {
		Resources []struct {
			model.LockableResource
			LabelsAsList []string `json:"labelsAsList"`
		} `json:"resources"`
	}
	if err := a.client.GetJSON(ctx, "lockable-resources/api/json", url.Values{"tree": {lockableResourceTree}}, &raw); err != nil {
		return nil, nil, unsupportedLockEndpoint(err, "Lockable Resources JSON API is unavailable", "lockable-resources/api/json")
	}
	if raw.Resources == nil {
		return nil, nil, apperrors.Wrap(apperrors.CodeUnsupported, "Jenkins did not export lockable resources", map[string]any{"endpoint": "lockable-resources/api/json"})
	}
	resources := make([]model.LockableResource, 0, len(raw.Resources))
	var warnings []string
	missingState := false
	for _, item := range raw.Resources {
		resource := item.LockableResource
		labels := item.LabelsAsList
		if labels == nil {
			labels = strings.Fields(resource.Labels)
		}
		resource.LabelList = make([]string, 0, len(labels))
		seen := map[string]bool{}
		for _, label := range labels {
			if label != "" && !seen[label] {
				resource.LabelList = append(resource.LabelList, label)
				seen[label] = true
			}
		}
		sort.Strings(resource.LabelList)
		missingState = missingState || resource.Free == nil || resource.Locked == nil || resource.Reserved == nil
		resources = append(resources, resource)
	}
	sort.SliceStable(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
	if missingState {
		warnings = append(warnings, "Some resource state fields were omitted by Jenkins; missing state is unknown and excluded from state counts and filters.")
	}
	return resources, warnings, nil
}

func unsupportedLockEndpoint(err error, message, endpoint string) error {
	var domain *apperrors.Error
	if errors.As(err, &domain) && domain.Code == apperrors.CodeNotFound {
		return apperrors.Wrap(apperrors.CodeUnsupported, message, map[string]any{"endpoint": endpoint})
	}
	return err
}

// Only simple names are safe to recognize across plugin argument renderers.
var simpleLockResource = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

func parseLockResource(description string) ([]string, bool) {
	value := strings.TrimSpace(description)
	value = strings.TrimPrefix(value, "Resource: ")
	if value == "nothing" || !simpleLockResource.MatchString(value) {
		return nil, true
	}
	return []string{value}, false
}

const (
	buildLockGraphTree   = "actions[_class,nodes[id,displayName]{0,1001}]"
	maxLockGraphNodes    = 1000
	maxBuildLockSteps    = 100
	flowGraphActionClass = "org.jenkinsci.plugins.workflow.job.views.FlowGraphAction"
)

func (a *API) BuildLockableResources(ctx context.Context, job string, build int) ([]model.BuildLockStep, []string, error) {
	// Query the build itself so missing builds retain not_found semantics.
	path := urlx.JobPath(job) + "/" + strconv.Itoa(build)
	var graph struct {
		Actions []struct {
			Class string `json:"_class"`
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"displayName"`
			} `json:"nodes"`
		} `json:"actions"`
	}
	if err := a.client.GetJSON(ctx, path+"/api/json", url.Values{"tree": {buildLockGraphTree}}, &graph); err != nil {
		return nil, nil, err
	}
	steps := make([]model.BuildLockStep, 0)
	warnings := []string{"Lock steps describe requests from the build flow graph and wfapi; current ownership and full lock history are unavailable through this source."}
	found := false
	seen := map[string]bool{}
	for _, action := range graph.Actions {
		if action.Class != flowGraphActionClass {
			continue
		}
		if action.Nodes == nil {
			return nil, nil, apperrors.Wrap(apperrors.CodeUnsupported, "Jenkins did not export Pipeline graph nodes", map[string]any{"source": "FlowGraphAction"})
		}
		found = true
		nodes := action.Nodes
		if len(nodes) == 0 {
			warnings = append(warnings, "The Pipeline graph has no nodes; execution metadata may not yet be available.")
		}
		if len(nodes) > maxLockGraphNodes {
			warnings = append(warnings, "Pipeline graph inspection was capped at 1000 nodes; additional lock requests may be omitted.")
			nodes = nodes[:maxLockGraphNodes]
		}
		for _, node := range nodes {
			if node.Name != "Lock shared resource : Start" && node.Name != "Lock shared resource" {
				continue
			}
			if seen[node.ID] {
				continue
			}
			seen[node.ID] = true
			if len(steps) == maxBuildLockSteps {
				warnings = append(warnings, "Lock detail inspection was capped at 100 steps; additional lock requests were omitted.")
				return steps, warnings, nil
			}
			if node.ID == "" {
				warnings = append(warnings, "A lock node was exported without an id; its details could not be read.")
				continue
			}
			var detail struct {
				Status               model.PipelineStatus `json:"status"`
				ParameterDescription string               `json:"parameterDescription"`
			}
			endpoint := path + "/execution/node/" + url.PathEscape(node.ID) + "/wfapi/describe"
			if err := a.client.GetJSON(ctx, endpoint, nil, &detail); err != nil {
				return nil, nil, unsupportedLockEndpoint(err, "Pipeline wfapi lock metadata is unavailable", endpoint)
			}
			names, ambiguous := parseLockResource(detail.ParameterDescription)
			steps = append(steps, model.BuildLockStep{NodeID: node.ID, Name: node.Name, Status: detail.Status, ParameterDescription: detail.ParameterDescription, ResourceNames: names, Ambiguous: ambiguous})
			if ambiguous {
				warning := "Lock node " + node.ID + " has an ambiguous parameter description; resource names were not inferred."
				if strings.TrimSpace(detail.ParameterDescription) == "" {
					warning = "Lock node " + node.ID + " exports no parameter description; label or expression requests cannot be resolved from wfapi."
				}
				warnings = append(warnings, warning)
			}
		}
	}
	if !found {
		return nil, nil, apperrors.Wrap(apperrors.CodeUnsupported, "Pipeline FlowGraphAction is unavailable for this build", map[string]any{"source": "FlowGraphAction"})
	}
	return steps, warnings, nil
}
