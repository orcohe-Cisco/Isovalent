package auth

import "testing"

func TestParseRoleClaim(t *testing.T) {
	tests := []struct {
		in    string
		ok    bool
		name  RoleName
		nsLen int
	}{
		{"ic:admin", true, RoleAdmin, 0},
		{"ic:viewer", true, RoleViewer, 0},
		{"ic:editor:shop,web", true, RoleEditor, 2},
		{"ic:editor:team-*", true, RoleEditor, 1},
		{"ic:root", false, "", 0},
		{"engineering", false, "", 0},
		{"ic:", false, "", 0},
	}
	for _, tt := range tests {
		role, ok := ParseRoleClaim(tt.in)
		if ok != tt.ok {
			t.Fatalf("%q: ok=%v want %v", tt.in, ok, tt.ok)
		}
		if !ok {
			continue
		}
		if role.Name != tt.name || len(role.Namespaces) != tt.nsLen {
			t.Fatalf("%q: got %+v", tt.in, role)
		}
	}
}

func TestIdentityScopes(t *testing.T) {
	editor := &Identity{Roles: []Role{{Name: RoleEditor, Namespaces: []string{"shop", "team-*"}}}}
	if !editor.CanEdit("shop") || !editor.CanEdit("team-pay") {
		t.Fatal("editor should edit scoped namespaces")
	}
	if editor.CanEdit("kube-system") {
		t.Fatal("editor must not edit out-of-scope namespace")
	}
	if editor.IsAdmin() {
		t.Fatal("editor is not admin")
	}
	if !editor.CanRead("shop") {
		t.Fatal("editor can read own scope")
	}

	admin := DevAdmin()
	if !admin.IsAdmin() || !admin.CanEdit("anything") || !admin.CanRead("") {
		t.Fatal("dev admin should have full access")
	}

	viewer := &Identity{Roles: []Role{{Name: RoleViewer}}}
	if viewer.CanEdit("shop") {
		t.Fatal("viewer must not edit")
	}
	if !viewer.CanRead("shop") {
		t.Fatal("viewer with no scopes reads everything")
	}
}
