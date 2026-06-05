// SPDX-License-Identifier: GPL-3.0-or-later

package smart

import (
	"testing"
)

// TestParseExtraFields asserts the newly modelled fields decode from the fixtures.
func TestParseExtraFields(t *testing.T) {
	ata := parseFixture(t, "smart-sda.json")
	if ata.PhysicalBlockSize == nil || *ata.PhysicalBlockSize != 4096 {
		t.Errorf("physical block size = %v, want 4096", ata.PhysicalBlockSize)
	}
	if ata.LogicalBlockSize == nil || *ata.LogicalBlockSize != 512 {
		t.Errorf("logical block size = %v, want 512", ata.LogicalBlockSize)
	}
	if ata.FormFactor == nil || ata.FormFactor.Name != "3.5 inches" {
		t.Errorf("form factor = %v", ata.FormFactor)
	}
	if ata.InterfaceSpeed == nil || ata.InterfaceSpeed.Current == nil || ata.InterfaceSpeed.Current.String != "6.0 Gb/s" {
		t.Errorf("interface speed not parsed: %+v", ata.InterfaceSpeed)
	}
	if ata.SATAVersion == nil || ata.SATAVersion.String != "SATA 3.3" {
		t.Errorf("sata version = %v", ata.SATAVersion)
	}
	if ata.Trim == nil || ata.Trim.Supported {
		t.Errorf("trim = %v, want supported=false for this HDD", ata.Trim)
	}
	if ata.ATASmartData == nil || ata.ATASmartData.SelfTest == nil || ata.ATASmartData.SelfTest.PollingMinutes == nil ||
		ata.ATASmartData.SelfTest.PollingMinutes.Extended != 1804 {
		t.Errorf("self-test polling not parsed: %+v", ata.ATASmartData)
	}
	if ata.SATAPhyEvents == nil || len(ata.SATAPhyEvents.Table) == 0 {
		t.Error("expected SATA PHY event counters")
	}

	nvme := parseFixture(t, "smart-nvme.json")
	if nvme.NVMeTotalCapacity == nil || *nvme.NVMeTotalCapacity != 2000398934016 {
		t.Errorf("nvme total capacity = %v", nvme.NVMeTotalCapacity)
	}
	if nvme.NVMeHealth == nil || nvme.NVMeHealth.HostReads != 520343530 {
		t.Errorf("host reads not parsed: %+v", nvme.NVMeHealth)
	}
	if nvme.NVMeHealth.ControllerBusyTime != 1131 {
		t.Errorf("controller busy = %d, want 1131", nvme.NVMeHealth.ControllerBusyTime)
	}
}
