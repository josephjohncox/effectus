package main

import (
	"flag"
	"fmt"
	"strings"
)

func validateDaemonMode() error {
	if *httpShutdownTimeout < 0 {
		return fmt.Errorf("--http-shutdown-timeout must not be negative")
	}
	if flag.NArg() != 0 {
		return fmt.Errorf("effectusd does not accept positional arguments")
	}
	if *runMode != "serve" && *runMode != "migrate" {
		return fmt.Errorf("--mode must be serve or migrate")
	}
	if !strings.EqualFold(*migrations, "apply") && !strings.EqualFold(*migrations, "validate") {
		return fmt.Errorf("--database-migrations must be validate or apply")
	}
	if *migrateOnly && *runMode != "serve" {
		return fmt.Errorf("choose --mode=migrate or the --migrate-only apply alias, not both")
	}
	if *migrateOnly || *runMode == "migrate" {
		if *bundleFile != "" || *ociRef != "" {
			return fmt.Errorf("migration mode does not accept --bundle or --oci-ref")
		}
		return nil
	}
	if (*bundleFile == "") == (*ociRef == "") {
		return fmt.Errorf("serve mode requires exactly one of --bundle or --oci-ref; use --mode=migrate for migrations")
	}
	if !strings.EqualFold(*factSource, "http") && !strings.EqualFold(*factSource, "kafka") {
		return fmt.Errorf("--fact-source must be http or kafka")
	}
	return nil
}
