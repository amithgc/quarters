package main

import (
	"fmt"

	"github.com/amithgc/quarters/internal/stats"
)

// ---- conversations ----

// Conversations lists a project's past Claude conversations, newest first;
// with query, those mentioning it.
func (a *App) Conversations(path, query string) ([]stats.Conversation, error) {
	if _, ok := a.project(path); !ok {
		return nil, fmt.Errorf("not one of your projects")
	}
	list := a.history.Search(projectDir(path), query, 80)
	if list == nil {
		list = []stats.Conversation{}
	}
	return list, nil
}

// ---- skills and MCP servers ----

// ToolsView is what Claude Code can use in a project.
type ToolsView struct {
	Skills []stats.Skill     `json:"skills"`
	MCP    []stats.MCPServer `json:"mcp"`
}

// ProjectTools lists a project's skills and MCP servers.
func (a *App) ProjectTools(path string) (ToolsView, error) {
	if _, ok := a.project(path); !ok {
		return ToolsView{}, fmt.Errorf("not one of your projects")
	}
	v := ToolsView{Skills: stats.Skills(path), MCP: stats.MCPServers(path)}
	if v.Skills == nil {
		v.Skills = []stats.Skill{}
	}
	if v.MCP == nil {
		v.MCP = []stats.MCPServer{}
	}
	return v, nil
}
