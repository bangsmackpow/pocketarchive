package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExtractPST shells out to readpst (libpst) and returns the paths of extracted
// .eml files. The caller owns the workspace and must remove it.
func ExtractPST(ctx context.Context, pstPath, workspace string, timeout time.Duration, log *slog.Logger) ([]string, error) {
	binary, err := exec.LookPath("readpst")
	if err != nil {
		return nil, errors.New("readpst is not installed; use the PocketArchive engine container image or install libpst/pst-utils")
	}
	outDir := filepath.Join(workspace, "pst-out")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return nil, err
	}
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	args := []string{"-e", "-o", outDir, pstPath}
	if log != nil {
		log.Info("extracting PST", "binary", binary, "path", pstPath)
	}
	cmd := exec.CommandContext(runCtx, binary, args...)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		if runCtx.Err() != nil {
			return nil, fmt.Errorf("readpst timed out or was cancelled: %w", runCtx.Err())
		}
		return nil, fmt.Errorf("readpst failed: %w: %s", err, truncate(string(output), 2000))
	}

	var files []string
	err = filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".eml") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("readpst produced no .eml files")
	}
	return files, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
