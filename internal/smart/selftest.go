// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import "time"

// SelfTestType is the kind of SMART self-test smartview can start; only short
// and long are exposed, deliberately.
type SelfTestType string

const (
	SelfTestShort SelfTestType = "short"
	SelfTestLong  SelfTestType = "long"
)

// SupportsSelfTest reports whether the drive can run SMART self-tests; gates
// the Tests tab. Drives that omit the relevant sections report false.
func (r *Report) SupportsSelfTest() bool {
	switch {
	case r.IsATA():
		return r.ATASmartData != nil && r.ATASmartData.Capabilities != nil &&
			r.ATASmartData.Capabilities.SelfTestsSupported
	case r.IsNVMe():
		// Some smartctl builds omit the optional-admin section; the self-test
		// log is only emitted for controllers that implement the command, so
		// its presence is an equally reliable signal.
		if r.NVMeOptAdmin != nil && r.NVMeOptAdmin.SelfTest {
			return true
		}
		return r.NVMeSelfTestLog != nil
	default:
		return false
	}
}

// SelfTestProgress reports an in-progress self-test for either protocol:
// percent done (0..100) and smartctl's status string.
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

// SelfTestDuration returns the advertised runtime for a self-test type; ATA
// only, NVMe returns false.
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
