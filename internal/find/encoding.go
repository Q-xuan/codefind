package find

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxEncodingProbeBytes = 1 << 20

var errStopEncodingProbe = errors.New("invalid utf-8 config table found")

func configTableRetryGlobs() []string {
	return append([]string{"*.csv", "*.tsv"}, searchExcludeGlobs()...)
}

func isConfigTableName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".csv" || ext == ".tsv"
}

func skipEncodingProbeDir(name string) bool {
	if name == "." || name == ".." {
		return false
	}
	if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" {
		return true
	}
	return false
}

// hasInvalidUTF8ConfigTables reports whether an authorized csv/tsv is not valid
// UTF-8. incomplete is true when the remaining timeout ran out before the walk
// finished; callers must not pretend a retry was applied.
func hasInvalidUTF8ConfigTables(ctx context.Context, root string, paths []string) (found bool, incomplete bool) {
	for _, rel := range paths {
		start := root
		if rel != "." {
			start = filepath.Join(root, filepath.FromSlash(rel))
		}
		err := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if skipEncodingProbeDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if !isConfigTableName(d.Name()) {
				return nil
			}
			if !inside(root, path) {
				return nil
			}
			invalid, readErr := fileHasInvalidUTF8Prefix(path)
			if readErr != nil {
				return nil
			}
			if invalid {
				found = true
				return errStopEncodingProbe
			}
			return nil
		})
		if errors.Is(err, errStopEncodingProbe) {
			return true, false
		}
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return found, true
			}
		}
	}
	if ctx.Err() != nil {
		return found, true
	}
	return found, false
}

func fileHasInvalidUTF8Prefix(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, maxEncodingProbeBytes)
	n, err := io.ReadFull(f, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		buf = buf[:n]
		err = nil
	}
	if err != nil {
		return false, err
	}
	return !utf8.Valid(buf), nil
}
