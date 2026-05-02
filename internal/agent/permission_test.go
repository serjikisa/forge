package agent

import "testing"

func TestPermissions_Defaults(t *testing.T) {
	p := NewPermissions()

	if got := p.Check("read_file"); got != PermAllow {
		t.Errorf("read_file = %d, want PermAllow", got)
	}
	if got := p.Check("list_directory"); got != PermAllow {
		t.Errorf("list_directory = %d, want PermAllow", got)
	}
	if got := p.Check("write_file"); got != PermAsk {
		t.Errorf("write_file = %d, want PermAsk", got)
	}
	if got := p.Check("shell_exec"); got != PermAsk {
		t.Errorf("shell_exec = %d, want PermAsk", got)
	}
	if got := p.Check("unknown_tool"); got != PermAsk {
		t.Errorf("unknown = %d, want PermAsk", got)
	}
}

func TestPermissions_AllowCategory(t *testing.T) {
	p := NewPermissions()

	if got := p.Check("shell_exec"); got != PermAsk {
		t.Fatalf("before: shell_exec = %d, want PermAsk", got)
	}

	p.AllowCategory("shell_exec")

	if got := p.Check("shell_exec"); got != PermAllow {
		t.Errorf("after: shell_exec = %d, want PermAllow", got)
	}
}

func TestPermissions_AllowCategoryAffectsSameCategory(t *testing.T) {
	p := NewPermissions()

	// Allow write_file — should also affect other file_write tools
	p.AllowCategory("write_file")

	if got := p.Check("write_file"); got != PermAllow {
		t.Errorf("write_file = %d, want PermAllow", got)
	}
	// shell_exec should still be Ask
	if got := p.Check("shell_exec"); got != PermAsk {
		t.Errorf("shell_exec = %d, want PermAsk (unchanged)", got)
	}
}

func TestPermissions_SessionOverridesDefault(t *testing.T) {
	p := NewPermissions()
	// read_file defaults to Allow, but session can override
	p.mu.Lock()
	p.session[CatFileRead] = PermDeny
	p.mu.Unlock()

	if got := p.Check("read_file"); got != PermDeny {
		t.Errorf("read_file = %d, want PermDeny (session override)", got)
	}
}

func TestCategoryName(t *testing.T) {
	tests := []struct {
		tool string
		want string
	}{
		{"read_file", "file_read"},
		{"write_file", "file_write"},
		{"shell_exec", "shell"},
		{"search_code", "file_read"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		if got := CategoryName(tt.tool); got != tt.want {
			t.Errorf("CategoryName(%q) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}
