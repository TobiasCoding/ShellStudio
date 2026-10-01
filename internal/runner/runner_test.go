// SPDX-License-Identifier: GPL-3.0-only
package runner

import (
	"strings"
	"testing"
	"time"
)

func TestTimeoutAndBoundedOutput(t *testing.T) {
	start := time.Now()
	_, e := Run(80*time.Millisecond, "", nil, "/bin/sh", "-c", "sleep 30")
	if e == nil || !strings.Contains(e.Error(), "timed out") {
		t.Fatal(e)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("timeout did not terminate the process group")
	}
	s, e := Run(5*time.Second, "", nil, "/bin/sh", "-c", "yes x | head -c 100000")
	if e != nil || len(s) != 65536 {
		t.Fatal(e, len(s))
	}
}
func TestArgumentsStayLiteral(t *testing.T) {
	want := "space ; $(echo hidden) ' quote"
	s, e := Run(time.Second, "", map[string]string{"SHELLSTUDIO_TEST": "literal"}, "/usr/bin/printf", "%s", want)
	if e != nil || s != want {
		t.Fatal(s, e)
	}
}
