package jenkins

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/pagination"
)

type ListLockableResourcesRequest struct {
	BaseRequest
	Name                string `json:"name,omitempty" jsonschema:"Exact case-sensitive resource name"`
	NameContains        string `json:"nameContains,omitempty" jsonschema:"Case-insensitive resource name substring"`
	Label               string `json:"label,omitempty" jsonschema:"Exact case-sensitive label name; expressions are not supported"`
	State               string `json:"state,omitempty" jsonschema:"Resource state filter: free, locked, reserved, unavailable (explicitly not free), or ephemeral"`
	IncludeLabelSummary *bool  `json:"includeLabelSummary,omitempty" jsonschema:"Include label counts for all matching resources before pagination; defaults to true"`
	Limit               int    `json:"limit,omitempty" jsonschema:"Maximum resources per page; defaults to 100 and is capped at 500"`
	Cursor              string `json:"cursor,omitempty" jsonschema:"Opaque next-page cursor; reuse with the same controller and filters"`
}
type ListLockableResourcesResponse struct {
	Resources  []model.LockableResource     `json:"resources" jsonschema:"Matching resources on this page, sorted by name"`
	Labels     []model.LockableLabelSummary `json:"labels,omitempty" jsonschema:"Label counts across all matching resources before pagination; omitted when disabled"`
	NextCursor string                       `json:"nextCursor,omitempty" jsonschema:"Opaque cursor for the next page"`
	HasMore    bool                         `json:"hasMore" jsonschema:"Whether matching resources remain after this page"`
	Truncated  bool                         `json:"truncated" jsonschema:"Whether matching resources were omitted from this page"`
	Limit      int                          `json:"limit" jsonschema:"Effective maximum resources per page"`
	Warnings   []string                     `json:"warnings,omitempty" jsonschema:"Limitations or missing resource fields"`
}

func ListLockableResources(ctx context.Context, deps Deps, in ListLockableResourcesRequest) (ListLockableResourcesResponse, error) {
	switch in.State {
	case "", "free", "locked", "reserved", "unavailable", "ephemeral":
	default:
		return ListLockableResourcesResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid lockable resource state", map[string]any{"state": in.State})
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return ListLockableResourcesResponse{}, err
	}
	signatureInput := in
	signatureInput.Controller = apiControllerID(deps, in.Controller)
	signatureInput.Cursor = ""
	signatureInput.Limit = 0
	signatureBytes, err := json.Marshal(signatureInput)
	if err != nil {
		return ListLockableResourcesResponse{}, err
	}
	signatureHash := sha256.Sum256(signatureBytes)
	signature := base64.RawURLEncoding.EncodeToString(signatureHash[:])
	const kind = "jenkins_list_lockable_resources"
	offset, gotSignature, err := pagination.DecodeCursor(in.Cursor, kind)
	if err != nil || (in.Cursor != "" && gotSignature != signature) {
		return ListLockableResourcesResponse{}, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid or mismatched lockable resources cursor", map[string]any{"tool": kind})
	}
	resources, warnings, err := api.ListLockableResources(ctx)
	if err != nil {
		return ListLockableResourcesResponse{}, err
	}
	matching := make([]model.LockableResource, 0, len(resources))
	for _, resource := range resources {
		if in.Name != "" && resource.Name != in.Name {
			continue
		}
		if in.NameContains != "" && !strings.Contains(strings.ToLower(resource.Name), strings.ToLower(in.NameContains)) {
			continue
		}
		if in.Label != "" && !hasLockLabel(resource.LabelList, in.Label) {
			continue
		}
		if !matchesLockState(resource, in.State) {
			continue
		}
		matching = append(matching, resource)
	}
	var labels []model.LockableLabelSummary
	if in.IncludeLabelSummary == nil || *in.IncludeLabelSummary {
		labels = summarizeLockLabels(matching)
	}
	limit := pagination.BoundLimit(in.Limit, 100, 500)
	if offset > len(matching) {
		offset = len(matching)
	}
	end := min(offset+limit, len(matching))
	hasMore := end < len(matching)
	next := ""
	if hasMore {
		next, err = pagination.EncodeCursor(kind, end, signature)
		if err != nil {
			return ListLockableResourcesResponse{}, err
		}
	}
	return ListLockableResourcesResponse{Resources: matching[offset:end], Labels: labels, NextCursor: next, HasMore: hasMore, Truncated: hasMore, Limit: limit, Warnings: warnings}, nil
}

func hasLockLabel(labels []string, label string) bool {
	for _, value := range labels {
		if value == label {
			return true
		}
	}
	return false
}
func lockStateTrue(value *bool) bool { return value != nil && *value }
func matchesLockState(r model.LockableResource, state string) bool {
	switch state {
	case "free":
		return lockStateTrue(r.Free)
	case "locked":
		return lockStateTrue(r.Locked)
	case "reserved":
		return lockStateTrue(r.Reserved)
	case "unavailable":
		return r.Free != nil && !*r.Free
	case "ephemeral":
		return lockStateTrue(r.Ephemeral)
	default:
		return true
	}
}
func summarizeLockLabels(resources []model.LockableResource) []model.LockableLabelSummary {
	counts := map[string]model.LockableLabelSummary{}
	for _, resource := range resources {
		for _, label := range resource.LabelList {
			summary := counts[label]
			summary.Name = label
			summary.ResourceCount++
			if lockStateTrue(resource.Free) {
				summary.FreeCount++
			}
			if lockStateTrue(resource.Locked) {
				summary.LockedCount++
			}
			if lockStateTrue(resource.Reserved) {
				summary.ReservedCount++
			}
			if resource.Free != nil && !*resource.Free {
				summary.UnavailableCount++
			}
			counts[label] = summary
		}
	}
	labels := make([]model.LockableLabelSummary, 0, len(counts))
	for _, summary := range counts {
		labels = append(labels, summary)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
	return labels
}

// Normalize omitted controller ids for both cursor binding and returned references.
func apiControllerID(deps Deps, controller string) string {
	if controller == "" {
		return deps.Config.DefaultController
	}
	return controller
}

type BuildLockableResourcesResponse struct {
	Build     model.BuildReference  `json:"build" jsonschema:"Configured controller and requested job/build reference"`
	Source    string                `json:"source" jsonschema:"Metadata source: pipeline_wfapi"`
	LockSteps []model.BuildLockStep `json:"lockSteps" jsonschema:"Lock step requests visible in exported Pipeline graph nodes; not a current ownership inventory"`
	Warnings  []string              `json:"warnings,omitempty" jsonschema:"Visibility limitations and ambiguous lock argument descriptions"`
}

func BuildLockableResources(ctx context.Context, deps Deps, in BuildRequest) (BuildLockableResourcesResponse, error) {
	if err := validateBuild(in.Job, in.Build); err != nil {
		return BuildLockableResourcesResponse{}, err
	}
	api, err := apiFor(deps, in.Controller)
	if err != nil {
		return BuildLockableResourcesResponse{}, err
	}
	steps, warnings, err := api.BuildLockableResources(ctx, in.Job, in.Build)
	if err != nil {
		return BuildLockableResourcesResponse{}, err
	}
	return BuildLockableResourcesResponse{Build: model.BuildReference{Controller: apiControllerID(deps, in.Controller), Job: in.Job, Build: in.Build, URL: replayBuildURL(api, in.Job, in.Build)}, Source: "pipeline_wfapi", LockSteps: steps, Warnings: warnings}, nil
}
