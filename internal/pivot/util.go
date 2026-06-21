package pivot

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func joinPath(root, rel string) string {
	rel = strings.TrimPrefix(rel, "/")
	return filepath.Join(root, rel)
}

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return nil
}
