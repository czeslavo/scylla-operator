// Package artifacts provides filesystem-backed implementations of the engine
// artifact interfaces (ArtifactWriter, ArtifactReader) and tar/gz archive
// utilities.
package artifacts

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/scylladb/scylla-operator/pkg/soda/engine"
)

// scopeDirName maps CollectorScope values to the kebab-case directory
// names used under the collectors/ prefix in the output directory.
var scopeDirName = map[engine.CollectorScope]string{
	engine.ClusterWide:      "cluster-wide",
	engine.PerScyllaCluster: "per-scylla-cluster",
	engine.PerScyllaNode:    "per-scylla-node",
}

// WriterFactory creates filesystem-backed ArtifactWriters.
type WriterFactory struct {
	baseDir string
}

// NewWriterFactory creates a new WriterFactory rooted at baseDir.
func NewWriterFactory(baseDir string) *WriterFactory {
	return &WriterFactory{baseDir: baseDir}
}

func (f *WriterFactory) NewWriter(collectorID engine.CollectorID, scope engine.CollectorScope, scopeKey engine.ScopeKey) engine.ArtifactWriter {
	scopeDir := scopeDirName[scope]
	var dir string
	if scopeKey.IsEmpty() {
		// ClusterWide scope: no scope key subdirectory.
		dir = filepath.Join(f.baseDir, "collectors", scopeDir, string(collectorID))
	} else {
		// PerScyllaCluster/PerScyllaNode: include namespace/name as path components.
		dir = filepath.Join(f.baseDir, "collectors", scopeDir, scopeKey.Namespace, scopeKey.Name, string(collectorID))
	}
	return &writer{dir: dir}
}

// writer implements engine.ArtifactWriter by writing to the filesystem.
type writer struct {
	dir string
}

func (w *writer) WriteArtifact(filename string, content []byte) (string, error) {
	path := filepath.Join(w.dir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating artifact directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("writing artifact %s: %w", path, err)
	}

	// Return just the filename as the relative path — relative to the
	// collector's own artifact directory (w.dir).
	return filename, nil
}

// Reader implements engine.ArtifactReader by reading from the
// filesystem layout produced by WriterFactory:
//
//	<baseDir>/collectors/cluster-wide/<collectorID>/<filename>
//	<baseDir>/collectors/per-scylla-cluster/<ns>/<name>/<collectorID>/<filename>
//	<baseDir>/collectors/per-scylla-node/<ns>/<name>/<collectorID>/<filename>
//
// It does not need to know the scope up-front — it probes the three possible
// locations and returns the first file found.
type Reader struct {
	baseDir string
}

var _ engine.ArtifactReader = (*Reader)(nil)

// NewReader creates a new artifact Reader rooted at baseDir.
func NewReader(baseDir string) *Reader {
	return &Reader{baseDir: baseDir}
}

// artifactDir returns the directory path for a given collector ID and scope key
// by probing the three possible scope directories.
func (r *Reader) artifactDir(collectorID engine.CollectorID, scopeKey engine.ScopeKey) string {
	if scopeKey.IsEmpty() {
		return filepath.Join(r.baseDir, "collectors", "cluster-wide", string(collectorID))
	}
	// Try per-scylla-node first (most specific), then per-scylla-cluster.
	perNode := filepath.Join(r.baseDir, "collectors", "per-scylla-node", scopeKey.Namespace, scopeKey.Name, string(collectorID))
	if _, err := os.Stat(perNode); err == nil {
		return perNode
	}
	return filepath.Join(r.baseDir, "collectors", "per-scylla-cluster", scopeKey.Namespace, scopeKey.Name, string(collectorID))
}

func (r *Reader) ReadArtifact(collectorID engine.CollectorID, scopeKey engine.ScopeKey, filename string) ([]byte, error) {
	path := filepath.Join(r.artifactDir(collectorID, scopeKey), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading artifact %s: %w", path, err)
	}
	return data, nil
}

func (r *Reader) ListArtifacts(collectorID engine.CollectorID, scopeKey engine.ScopeKey) ([]engine.Artifact, error) {
	dir := r.artifactDir(collectorID, scopeKey)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing artifacts in %s: %w", dir, err)
	}

	var result []engine.Artifact
	for _, entry := range entries {
		if !entry.IsDir() {
			result = append(result, engine.Artifact{
				RelativePath: entry.Name(),
			})
		}
	}
	return result, nil
}

// CreateTarGz creates a .tar.gz archive at destPath containing all files under
// srcDir. The archive entries use paths relative to srcDir.
func CreateTarGz(srcDir, destPath string) error {
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating archive file: %w", err)
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Compute the path inside the archive relative to srcDir.
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("creating tar header for %s: %w", path, err)
		}
		header.Name = filepath.ToSlash(rel)

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing tar header for %s: %w", path, err)
		}

		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("opening file %s: %w", path, err)
		}
		defer f.Close()

		if _, err := io.Copy(tw, f); err != nil {
			return fmt.Errorf("writing file %s to archive: %w", path, err)
		}

		return nil
	})
}

// ExtractTarGz extracts a .tar.gz archive to the given destination directory.
func ExtractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Sanitise path to prevent directory traversal attacks.
		target := filepath.Join(dest, filepath.Clean("/"+header.Name))
		if !strings.HasPrefix(target, dest+string(os.PathSeparator)) && target != dest {
			return fmt.Errorf("tar entry %q would escape destination directory", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", target, err)
			}
			out, err := os.Create(target)
			if err != nil {
				return fmt.Errorf("creating file %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return fmt.Errorf("writing file %s: %w", target, err)
			}
			out.Close()
		}
	}
	return nil
}
