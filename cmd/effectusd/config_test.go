package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadRuntimeConfigRejectsUnknownFields(t *testing.T) {
	for _, test := range []struct {
		name    string
		ext     string
		content string
	}{
		{name: "yaml", ext: ".yaml", content: "bundle:\n  file: bundle.json\n  typo: true\n"},
		{name: "json", ext: ".json", content: `{"bundle":{"file":"bundle.json","typo":true}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config"+test.ext)
			require.NoError(t, os.WriteFile(path, []byte(test.content), 0600))
			_, err := loadRuntimeConfig(path)
			require.ErrorContains(t, err, "typo")
		})
	}
}

func TestLoadRuntimeConfigRejectsMultipleDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("bundle: {}\n---\nhttp: {}\n"), 0600))
	_, err := loadRuntimeConfig(path)
	require.ErrorContains(t, err, "multiple configuration documents")
}
