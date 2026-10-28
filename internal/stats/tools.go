package stats

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Skill is a Claude Code skill available in a project.
type Skill struct {
	Name  string `json:"name"`
	About string `json:"about"`
	Scope string `json:"scope"` // project, user or the plugin's name
}

// MCPServer is an MCP server configured for a project. Only harmless
// details are kept: never its environment, headers or URL query (secrets).
type MCPServer struct {
	Name   string `json:"name"`
	Scope  string `json:"scope"` // user, local, project or the plugin's name
	Type   string `json:"type"`  // stdio, http, sse
	Target string `json:"target"`
}

// Skills lists the skills Claude Code finds for a project: its own
// (.claude/skills), the user's (~/.claude/skills) and installed plugins'.
func Skills(projectPath string) []Skill {
	home, _ := os.UserHomeDir()
	var out []Skill
	seen := map[string]bool{}
	scan := func(dir, scope string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
				continue
			}
			name, about, ok := skillHeader(filepath.Join(dir, e.Name(), "SKILL.md"))
			if !ok {
				continue
			}
			if name == "" {
				name = e.Name()
			}
			if seen[scope+"/"+name] {
				continue
			}
			seen[scope+"/"+name] = true
			out = append(out, Skill{Name: name, About: clip(about, 200), Scope: scope})
		}
	}
	scan(filepath.Join(projectPath, ".claude", "skills"), "project")
	scan(filepath.Join(home, ".claude", "skills"), "user")
	for _, p := range plugins(home, projectPath) {
		scan(filepath.Join(p.path, "skills"), p.name)
	}
	return out
}

type plugin struct{ name, path string }

// plugins are the installed Claude Code plugins that apply to projectPath.
func plugins(home, projectPath string) []plugin {
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Plugins map[string][]struct {
			Scope       string `json:"scope"`
			InstallPath string `json:"installPath"`
			ProjectPath string `json:"projectPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(data, &f) != nil {
		return nil
	}
	var out []plugin
	for id, list := range f.Plugins {
		name, _, _ := strings.Cut(id, "@")
		for _, p := range list {
			if p.Scope == "user" || p.ProjectPath == projectPath {
				out = append(out, plugin{name, p.InstallPath})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// skillHeader reads name and description from a SKILL.md front matter.
func skillHeader(path string) (name, about string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", "", true
	}
	for i := 0; i < 60 && sc.Scan(); i++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.TrimSpace(k) {
		case "name":
			name = v
		case "description":
			about = v
		}
	}
	return name, about, true
}

type mcpEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	URL     string   `json:"url"`
}

// MCPServers lists the MCP servers for a project: the user's and the
// project's entries in ~/.claude.json, the project's .mcp.json and
// installed plugins'.
func MCPServers(projectPath string) []MCPServer {
	home, _ := os.UserHomeDir()
	var out []MCPServer
	seen := map[string]bool{}
	add := func(m map[string]mcpEntry, scope string) {
		names := make([]string, 0, len(m))
		for k := range m {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			if seen[k] {
				continue
			}
			seen[k] = true
			e := m[k]
			s := MCPServer{Name: k, Scope: scope, Type: e.Type}
			if e.URL != "" {
				if s.Type == "" {
					s.Type = "http"
				}
				if u, err := url.Parse(e.URL); err == nil {
					s.Target = u.Host + u.Path
				}
			} else {
				if s.Type == "" {
					s.Type = "stdio"
				}
				s.Target = safeCommand(e.Command, e.Args)
			}
			out = append(out, s)
		}
	}
	read := func(path string, v any) bool {
		data, err := os.ReadFile(path)
		return err == nil && json.Unmarshal(data, v) == nil
	}
	var cfg struct {
		MCPServers map[string]mcpEntry `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]mcpEntry `json:"mcpServers"`
		} `json:"projects"`
	}
	if read(filepath.Join(home, ".claude.json"), &cfg) {
		add(cfg.Projects[projectPath].MCPServers, "local")
	}
	var proj struct {
		MCPServers map[string]mcpEntry `json:"mcpServers"`
	}
	if read(filepath.Join(projectPath, ".mcp.json"), &proj) {
		add(proj.MCPServers, "project")
	}
	add(cfg.MCPServers, "user")
	for _, p := range plugins(home, projectPath) {
		var pm struct {
			MCPServers map[string]mcpEntry `json:"mcpServers"`
		}
		if read(filepath.Join(p.path, ".mcp.json"), &pm) {
			add(pm.MCPServers, p.name)
		}
	}
	return out
}

var secretish = regexp.MustCompile(`(?i)(key|token|secret|password|bearer|auth)|^[A-Za-z0-9_\-]{32,}$`)

// safeCommand shows a server's command without anything that may be a secret.
func safeCommand(cmd string, args []string) string {
	parts := []string{filepath.Base(cmd)}
	for _, a := range args {
		if len(parts) >= 3 || len(a) > 60 || secretish.MatchString(a) || strings.Contains(a, "=") {
			break
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if strings.HasPrefix(a, "/") || strings.HasPrefix(a, "~") {
			a = filepath.Base(a) // a path: its name is enough
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
