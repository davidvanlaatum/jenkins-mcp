package mcpserver

import (
	"encoding/json"
	"testing"

	jenkinstools "github.com/david/jenkins-mcp/internal/tools/jenkins"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestPipelineInputPayloadRedaction(t *testing.T) {
	r := require.New(t)
	in := jenkinstools.ProceedPipelineInputRequest{PipelineInputRequest: jenkinstools.PipelineInputRequest{Job: "app", Build: 1, InputID: "Approve"}, Parameters: map[string]any{"arbitrary": "private-value"}}
	raw, err := json.Marshal(in)
	r.NoError(err)
	for _, req := range []*mcp.CallToolRequest{nil, {Params: &mcp.CallToolParamsRaw{Arguments: raw}}} {
		payload := requestPayload(req, in)
		r.NotContains(payload, "private-value")
		r.NotContains(payload, "arbitrary")
		r.Contains(payload, "[REDACTED]")
		r.Contains(payload, "Approve")
	}
}
