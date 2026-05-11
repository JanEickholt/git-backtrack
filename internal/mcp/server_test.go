package mcp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeHandlesInitializeAndToolsList(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}, "\n") + "\n"

	var stdout bytes.Buffer
	if err := Serve(strings.NewReader(input), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("serve: %v", err)
	}
	responses := decodeResponses(t, stdout.String())
	if len(responses) != 2 {
		t.Fatalf("responses len = %d, want 2: %s", len(responses), stdout.String())
	}
	if responses[0]["error"] != nil {
		t.Fatalf("initialize error: %+v", responses[0])
	}

	result, ok := responses[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list result = %+v", responses[1]["result"])
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) != 4 {
		t.Fatalf("tools = %+v, want 4 tools", result["tools"])
	}
	if !containsTool(tools, "git_backtrack_help") || !containsTool(tools, "git_backtrack_apply") {
		t.Fatalf("tools missing expected entries: %+v", tools)
	}
}

func TestServeCallsHelpTool(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"git_backtrack_help","arguments":{}}}` + "\n"

	var stdout bytes.Buffer
	if err := Serve(strings.NewReader(input), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("serve: %v", err)
	}
	responses := decodeResponses(t, stdout.String())
	if len(responses) != 1 {
		t.Fatalf("responses len = %d, want 1", len(responses))
	}
	result := responses[0]["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "git-backtrack tool mode") || !strings.Contains(text, "recommended_sequence") {
		t.Fatalf("help tool text missing contract: %s", text)
	}
}

func decodeResponses(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	responses := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		responses = append(responses, response)
	}
	return responses
}

func containsTool(tools []any, name string) bool {
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if ok && tool["name"] == name {
			return true
		}
	}
	return false
}
