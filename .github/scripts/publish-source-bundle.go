// Command publish-source-bundle publishes an already-validated SourceBundle.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/josephjohncox/effectus/bundle"
)

func main() {
	sourcePath := flag.String("bundle", "", "Path to a SourceBundle JSON file")
	ociRef := flag.String("oci-ref", "", "OCI reference to publish")
	flag.Parse()
	if *sourcePath == "" || *ociRef == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run .github/scripts/publish-source-bundle.go --bundle FILE --oci-ref REFERENCE")
		os.Exit(2)
	}
	data, err := os.ReadFile(*sourcePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read source bundle:", err)
		os.Exit(1)
	}
	source, err := bundle.Parse(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parse source bundle:", err)
		os.Exit(1)
	}
	digest, err := source.PublishOCI(context.Background(), *ociRef)
	if err != nil {
		fmt.Fprintln(os.Stderr, "publish source bundle:", err)
		os.Exit(1)
	}
	fmt.Println(digest)
}
