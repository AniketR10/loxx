package shell

import (
	"strings"
	"testing"
)

func TestScript(t *testing.T) {
	for _, sh := range []string{"bash", "zsh"} {
		s, err := Script(sh, "/opt/it's here/loc")
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !strings.Contains(s, `_loc_bin='/opt/it'\''s here/loc'`) {
			t.Errorf("%s: binary path not quoted into the script", sh)
		}
		for _, placeholder := range []string{"__LOC_BIN__", "__BASH_PREEXEC__", "__BASH_PREEXEC_LICENSE__"} {
			if strings.Contains(s, placeholder) {
				t.Errorf("%s: placeholder %s left in the script", sh, placeholder)
			}
		}
	}

	bash, _ := Script("bash", "/usr/bin/loc")
	if !strings.Contains(bash, "# Copyright (c) 2017 Ryan Caloras") || !strings.Contains(bash, "# Permission is hereby granted") {
		t.Error("bash script must carry bash-preexec's MIT notice")
	}
	if !strings.Contains(bash, "__bp_install_after_session_init") {
		t.Error("bash script is missing bash-preexec itself")
	}
	if strings.Count(bash, bashPreexecEnd) != 2 { // opening and closing of the here-document
		t.Errorf("here-document delimiter appears %d times, want 2", strings.Count(bash, bashPreexecEnd))
	}

	if _, err := Script("fish", "/usr/bin/loc"); err == nil {
		t.Error("expected an error for an unsupported shell")
	}
}
