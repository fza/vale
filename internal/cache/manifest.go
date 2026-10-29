package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const manifestFormat = 1

// manifest lists the segments, oldest first. It is replaced whole, by a rename,
// under the writer lock, so a reader sees one version or the next.
type manifest struct {
	Format   int           `json:"format"`
	Segments []segmentInfo `json:"segments"`
}

// readManifest returns the manifest in dir. A missing or unreadable one lists
// nothing, which leaves its segments to the sweep that collects strays.
func readManifest(dir string) (manifest, error) {
	content, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return manifest{Format: manifestFormat}, nil
		}
		return manifest{}, err
	}

	var m manifest
	if err = json.Unmarshal(content, &m); err != nil {
		return manifest{Format: manifestFormat}, nil //nolint:nilerr // a damaged manifest lists nothing
	}
	if m.Format != manifestFormat {
		return manifest{}, errUnknownFormat
	}

	return m, nil
}

// writeManifest replaces the manifest in dir. The caller holds the writer lock.
func writeManifest(dir string, m manifest) error {
	m.Format = manifestFormat

	content, err := json.Marshal(m)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(dir, "manifest-*.tmp")
	if err != nil {
		return err
	}

	_, err = f.Write(content)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, manifestName))
	}
	if err != nil {
		os.Remove(f.Name())
	}

	return err
}
