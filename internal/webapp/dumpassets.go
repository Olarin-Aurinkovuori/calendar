package webapp

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// DumpEmbeddedAssets writes every built-in template and static asset file
// -- exactly as compiled into the binary, ignoring any on-disk overrides --
// to destDir, preserving their "templates/..."/"static/..." relative
// paths. destDir must not already exist, to avoid silently merging into or
// clobbering an existing directory.
func DumpEmbeddedAssets(destDir string) error {
	if err := mkdirExclusive(destDir); err != nil {
		return err
	}
	if err := writeAssetTree(embeddedTemplateFS, destDir, embeddedTemplateFS.Open); err != nil {
		return err
	}
	return writeAssetTree(embeddedStaticFS, destDir, embeddedStaticFS.Open)
}

// DumpEffectiveAssets writes every template and static asset file as the
// running server actually resolves it -- an on-disk override under the
// current working directory, if present, otherwise the built-in default --
// to destDir. destDir must not already exist.
func DumpEffectiveAssets(destDir string) error {
	if err := mkdirExclusive(destDir); err != nil {
		return err
	}
	if err := writeAssetTree(embeddedTemplateFS, destDir, templateFS.Open); err != nil {
		return err
	}
	return writeAssetTree(embeddedStaticFS, destDir, staticFS.Open)
}

// mkdirExclusive creates dir, failing if it already exists, so a dump
// never silently merges into or overwrites an existing directory.
func mkdirExclusive(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("directory %q already exists", dir)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

// writeAssetTree walks every regular file known to catalog (the embedded
// filesystem, used only to enumerate the canonical set of asset paths),
// opens each path's content via open (which may resolve to an on-disk
// override instead of the embedded copy), and writes it under destDir at
// the same relative path.
func writeAssetTree(catalog fs.FS, destDir string, open func(name string) (fs.File, error)) error {
	return fs.WalkDir(catalog, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		f, oerr := open(path)
		if oerr != nil {
			return fmt.Errorf("opening %s: %w", path, oerr)
		}
		defer f.Close()
		data, rerr := io.ReadAll(f)
		if rerr != nil {
			return fmt.Errorf("reading %s: %w", path, rerr)
		}
		target := filepath.Join(destDir, filepath.FromSlash(path))
		if merr := os.MkdirAll(filepath.Dir(target), 0o755); merr != nil {
			return merr
		}
		return os.WriteFile(target, data, 0o644)
	})
}
