// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "time"

// SelfTestType is the kind of SMART self-test smartview can start. smartctl also
// accepts conveyance and selective tests, but smartview deliberately exposes only
// the short and long (extended) variants; using a named type keeps the valid set
// discoverable and compiler-checked at call sites instead of passing raw strings.
type SelfTestType string

const (
	SelfTestShort SelfTestType = "short"
	SelfTestLong  SelfTestType = "long"
)

// SupportsSelfTest reports whether the drive can run SMART self-tests, gating
// the Tests tab. ATA exposes the capability bit directly; NVMe self-test is an
// optional admin command. Drives that omit these sections (e.g. Apple internal
// NVMe) decode to nil and report false.
func (r *Report) SupportsSelfTest() bool {
	switch {
	case r.IsATA():
		return r.ATASmartData != nil && r.ATASmartData.Capabilities != nil &&
			r.ATASmartData.Capabilities.SelfTestsSupported
	case r.IsNVMe():
		// The optional-admin bit is authoritative, but some smartctl
		// builds omit that section; smartctl only emits the self-test log
		// section for controllers that implement the command, so its
		// presence is an equally reliable support signal.
		if r.NVMeOptAdmin != nil && r.NVMeOptAdmin.SelfTest {
			return true
		}
		return r.NVMeSelfTestLog != nil
	default:
		return false
	}
}

// SelfTestProgress reports an in-progress self-test. running is true only while
// a test executes; percent is 0..100 done, and label is smartctl's status
// string (e.g. "Extended offline" / the NVMe operation name) when available.
// Both protocols are unified here so the UI need not branch.
func (r *Report) SelfTestProgress() (label string, percent int, running bool) {
	switch {
	case r.IsATA():
		if r.ATASmartData == nil || r.ATASmartData.SelfTest == nil {
			return "", 0, false
		}
		s := r.ATASmartData.SelfTest.Status
		if s == nil || s.RemainingPercent == nil {
			return "", 0, false
		}
		done := 100 - *s.RemainingPercent
		if done < 0 {
			done = 0
		}
		if done > 100 {
			done = 100
		}
		return s.String, done, true
	case r.IsNVMe():
		l := r.NVMeSelfTestLog
		if l == nil || l.CurrentSelfTestOperation == nil || l.CurrentSelfTestOperation.Value == 0 {
			return "", 0, false
		}
		done := 0
		if l.CurrentCompletionPercent != nil {
			done = *l.CurrentCompletionPercent
		}
		return l.CurrentSelfTestOperation.String, done, true
	default:
		return "", 0, false
	}
}

// SelfTestDuration returns the estimated runtime for a self-test type, when the
// drive advertises it. Only ATA reports polling minutes; NVMe returns ok=false.
func (r *Report) SelfTestDuration(testType SelfTestType) (time.Duration, bool) {
	if r.ATASmartData == nil || r.ATASmartData.SelfTest == nil || r.ATASmartData.SelfTest.PollingMinutes == nil {
		return 0, false
	}
	p := r.ATASmartData.SelfTest.PollingMinutes
	var min int
	switch testType {
	case SelfTestShort:
		min = p.Short
	case SelfTestLong:
		min = p.Extended
	default:
		return 0, false
	}
	if min <= 0 {
		return 0, false
	}
	return time.Duration(min) * time.Minute, true
}
