// SPDX-License-Identifier: GPL-3.0-or-later

// Command redact-fixture replaces the drive identifiers in smartctl JSON dumps,
// in place, with the placeholders smartview's committed fixtures use.
//
//	go run ./cmd/redact-fixture -serial EXAMPLE-ST4000 -device /dev/sdi smart-st4000.json smart-st4000-farm.json
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/trm42/smartview/internal/redact"
)

func main() {
	serial := flag.String("serial", "", "placeholder serial for this drive, e.g. EXAMPLE-ST4000 (required; pass the same one for its FARM log)")
	device := flag.String("device", "", "optional new device.name, so the fixture does not collide with one already committed")
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "usage: redact-fixture -serial EXAMPLE-<MODEL> [-device /dev/sdX] FILE.json...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *serial == "" || flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	failed := false
	for _, path := range flag.Args() {
		if err := redactFile(path, *serial, *device); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func redactFile(path, serial, device string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the user names the file to rewrite
	if err != nil {
		return err
	}
	out, err := redact.Redact(data, serial)
	if err != nil {
		return err
	}
	if device != "" {
		if out, err = redact.Rename(out, device); err != nil {
			return err
		}
	}
	findings, err := redact.Check(out)
	if err != nil {
		return err
	}
	if len(findings) > 0 {
		return fmt.Errorf("identifiers left after redaction, file not written: %v", findings)
	}
	if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil { // #nosec G703 -- same reason
		return err
	}
	fmt.Printf("%s: redacted\n", path)
	return nil
}
