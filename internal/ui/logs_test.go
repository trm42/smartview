// SPDX-License-Identifier: GPL-3.0-or-later

package ui

import (
	"strings"
	"testing"

	"github.com/trm42/smartview/internal/smart"
)

// TestNVMeErrorCountIsEntriesNotCapacity pins the fix for a real bug: the NVMe
// error information log reports its slot capacity in "size" (256 on every drive
// smartctl has been seen to read), and the Logs tab printed that as an error
// count — "256 entries (253 unread)" above three decoded entries, on a drive
// whose Attributes tab said 3.
func TestNVMeErrorCountIsEntriesNotCapacity(t *testing.T) {
	r := &smart.Report{
		Device: smart.Device{Protocol: "NVMe"},
		NVMeErrorLog: &smart.NVMeErrorLog{
			Size: 256, Read: 3, Unread: 0,
			Table: []smart.NVMeErrorLogEntry{{}, {}, {}},
		},
	}
	got := buildLogsText(r)
	if !strings.Contains(got, "3 errors logged") {
		t.Errorf("want %q in logs text, got:\n%s", "3 errors logged", got)
	}
	for _, unwanted := range []string{"256", "253"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("log capacity %s leaked into the error count:\n%s", unwanted, got)
		}
	}
}

// TestNVMeUnreadComesFromSmartctl checks the unread figure is smartctl's own and
// is not derived by subtracting Read from the capacity.
func TestNVMeUnreadComesFromSmartctl(t *testing.T) {
	r := &smart.Report{
		Device: smart.Device{Protocol: "NVMe"},
		NVMeErrorLog: &smart.NVMeErrorLog{
			Size: 256, Read: 2, Unread: 1,
			Table: []smart.NVMeErrorLogEntry{{}, {}},
		},
	}
	got := buildLogsText(r)
	if !strings.Contains(got, "2 errors logged") || !strings.Contains(got, "1 not read back") {
		t.Errorf("want 2 logged and 1 unread, got:\n%s", got)
	}
}

// TestErrorCountPlural checks the "error(s)" placeholder is gone on both paths.
func TestErrorCountPlural(t *testing.T) {
	one := &smart.Report{
		Device:       smart.Device{Protocol: "NVMe"},
		NVMeErrorLog: &smart.NVMeErrorLog{Size: 256, Table: []smart.NVMeErrorLogEntry{{}}},
	}
	got := buildLogsText(one)
	if !strings.Contains(got, "1 error logged") || strings.Contains(got, "error(s)") {
		t.Errorf("want singular %q and no placeholder plural, got:\n%s", "1 error logged", got)
	}
}
