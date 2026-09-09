package schema

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

func sortedBufFieldNames(fields map[string]interface{}) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// existingBufProto never rewrites an existing wire definition.
func existingBufProto(root *os.Root, path, content string) (bool, error) {
	info, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return true, fmt.Errorf("protobuf output must be a regular file")
	}
	data, err := root.ReadFile(path)
	if err != nil {
		return true, err
	}
	if string(data) != content {
		return true, fmt.Errorf("protobuf file already exists with a different definition. Preserve its field numbers and use an explicit Buf migration")
	}
	return true, nil
}

// installBufProto installs a complete new file without overwriting any target.
// os.Root confines intermediate symlinks to the chosen workspace.
func (b *BufIntegration) installBufProto(ctx context.Context, path, content string) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	rel, err := filepath.Rel(b.workspaceRoot, path)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(b.workspaceRoot, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(b.workspaceRoot)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	if err = root.MkdirAll(filepath.Dir(rel), 0755); err != nil {
		return err
	}
	if exists, err := existingBufProto(root, rel, content); exists || err != nil {
		return err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(rel), ".buf-"+hex.EncodeToString(random[:])+".tmp")
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
		err = errors.Join(err, root.Remove(temporary))
	}()
	written, err := file.WriteString(content)
	if err != nil {
		return err
	}
	if written != len(content) {
		return io.ErrShortWrite
	}
	if err = file.Sync(); err != nil {
		return err
	}
	err = file.Close()
	closed = true
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = root.Link(temporary, rel)
	if errors.Is(err, os.ErrExist) {
		_, err = existingBufProto(root, rel, content)
	}
	return err
}
