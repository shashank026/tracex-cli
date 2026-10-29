package gitrepo

import "testing"

func TestParseRemote(t *testing.T) {
	for remote, want := range map[string]string{
		"git@github.com:acme/api.git":       "acme/api",
		"https://github.com/acme/api":       "acme/api",
		"https://github.com/acme/api.git/":  "acme/api",
		"ssh://git@github.com/acme/api.git": "acme/api",
		"work-alias:acme/api.git":           "acme/api", // an SSH host alias
		"https://token@github.com/acme/api": "acme/api",
	} {
		owner, name, ok := ParseRemote(remote)
		if !ok || owner+"/"+name != want {
			t.Errorf("ParseRemote(%q) = %q/%q %v, want %s", remote, owner, name, ok, want)
		}
	}
	if _, _, ok := ParseRemote("not-a-remote"); ok {
		t.Error("a remote without owner/name should not parse")
	}
}
