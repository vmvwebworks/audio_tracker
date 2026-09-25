// Package testutil provides fixtures shared by the tests.
package testutil

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

// Root returns the repository root.
func Root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// SoundFont returns the SoundFont path, skipping the test if it has not been
// downloaded (run scripts/setup.ps1).
func SoundFont(t testing.TB) string {
	t.Helper()
	p := filepath.Join(Root(), "assets", "GeneralUser-GS.sf2")
	if _, err := os.Stat(p); err != nil {
		t.Skip("falta assets/GeneralUser-GS.sf2: ejecuta scripts/setup.ps1")
	}
	return p
}

// FixtureGP writes the minimal test score as a .gp file (a zip holding
// Content/score.gpif) into dir and returns its path.
func FixtureGP(t testing.TB, dir string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(Root(), "internal", "gp", "testdata", "minimal.gpif"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Prueba mínima.gp")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("Content/score.gpif")
	if err == nil {
		_, err = w.Write(src)
	}
	if err == nil {
		err = zw.Close()
	}
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// Song loads the minimal test score. When GP_SAMPLE points to a real .gp
// file, that file is used instead.
func Song(t testing.TB) *gp.Song {
	t.Helper()
	path := os.Getenv("GP_SAMPLE")
	if path == "" {
		path = FixtureGP(t, t.TempDir())
	}
	s, err := gp.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
