package portalserver

import (
	"archive/zip"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// plagViewerPage reports whether path is a client-side route of the JPlag
// report viewer. The viewer's router uses root-relative paths, so these pages
// live at the origin root next to the portal's own SPA routes.
func plagViewerPage(path string) bool {
	switch path {
	case "/overview", "/info":
		return true
	}
	for _, prefix := range []string{"/comparison/", "/cluster/", "/old/", "/error/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// extractPlagViewer unpacks the report viewer bundled in the JPlag jar into
// <jar dir>/report-viewer and returns that directory, or "" when the jar is
// missing or has no viewer inside.
func extractPlagViewer(jarPath string) string {
	r, err := zip.OpenReader(jarPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("report viewer: open %s: %v", jarPath, err)
		}
		return ""
	}
	defer r.Close()

	destDir := filepath.Join(filepath.Dir(jarPath), "report-viewer")
	extracted := 0
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, "report-viewer/") || f.FileInfo().IsDir() {
			continue
		}
		rel := filepath.FromSlash(strings.TrimPrefix(f.Name, "report-viewer/"))
		if rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		data, err := readZipEntry(f)
		if err != nil {
			log.Printf("report viewer: read %s: %v", f.Name, err)
			return ""
		}
		dst := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			log.Printf("report viewer: %v", err)
			return ""
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			log.Printf("report viewer: write %s: %v", rel, err)
			return ""
		}
		extracted++
	}
	if extracted == 0 {
		log.Printf("report viewer: jar %s has no bundled viewer", jarPath)
		return ""
	}
	return destDir
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 64<<20))
}
