// permission.go manages per-category tool permissions (ask, allow, deny) with
// session-level overrides for the "always allow" workflow.
package agent

import "sync"

// Permission controls whether a tool category requires confirmation.
type Permission int

const (
	PermAsk   Permission = iota // prompt user each time
	PermAllow                   // auto-approve
	PermDeny                    // auto-deny
)

// Category groups tools by their access pattern.
type Category string

const (
	CatFileRead  Category = "file_read"
	CatFileWrite Category = "file_write"
	CatShell     Category = "shell"
	CatWeb       Category = "web"
)

// toolCategory maps tool names to permission categories.
var toolCategory = map[string]Category{
	"read_file":      CatFileRead,
	"list_directory": CatFileRead,
	"search_code":    CatFileRead,
	"write_file":     CatFileWrite,
	"shell_exec":     CatShell,
	"web_search":     CatWeb,
	"web_fetch":      CatWeb,
}

// Permissions tracks per-category permission state with session overrides.
type Permissions struct {
	mu       sync.Mutex
	defaults map[Category]Permission
	session  map[Category]Permission
}

func NewPermissions() *Permissions {
	return &Permissions{
		defaults: map[Category]Permission{
			CatFileRead:  PermAllow,
			CatFileWrite: PermAsk,
			CatShell:     PermAsk,
			CatWeb:       PermAsk,
		},
		session: make(map[Category]Permission),
	}
}

// Check returns the effective permission for a tool.
func (p *Permissions) Check(toolName string) Permission {
	cat, ok := toolCategory[toolName]
	if !ok {
		return PermAsk
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if perm, ok := p.session[cat]; ok {
		return perm
	}
	if perm, ok := p.defaults[cat]; ok {
		return perm
	}
	return PermAsk
}

// AllowCategory sets a session-level override to always allow a category.
func (p *Permissions) AllowCategory(toolName string) {
	cat, ok := toolCategory[toolName]
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.session[cat] = PermAllow
}

// CategoryName returns the human-readable category for a tool.
func CategoryName(toolName string) string {
	if cat, ok := toolCategory[toolName]; ok {
		return string(cat)
	}
	return "unknown"
}
