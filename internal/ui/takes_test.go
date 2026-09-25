package ui

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmvwebworks/audio_tracker/internal/gp"
)

// writeWav writes a tiny valid mono 16-bit WAV.
func writeWav(t *testing.T, path string) {
	t.Helper()
	h := make([]byte, 44+200)
	le := binary.LittleEndian
	copy(h, "RIFF")
	le.PutUint32(h[4:], uint32(len(h)-8))
	copy(h[8:], "WAVEfmt ")
	le.PutUint32(h[16:], 16)
	le.PutUint16(h[20:], 1)
	le.PutUint16(h[22:], 1)
	le.PutUint32(h[24:], 44100)
	le.PutUint32(h[28:], 88200)
	le.PutUint16(h[32:], 2)
	le.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	le.PutUint32(h[40:], 200)
	if err := os.WriteFile(path, h, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTakesManagement(t *testing.T) {
	dir := t.TempDir()
	a := &App{song: &gp.Song{}, path: filepath.Join(dir, "cancion.gp")}
	if err := os.MkdirAll(a.takesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(a.takesDir(), "toma 2026-09-25 11.29.20 - compás 1 (0-00).wav")
	writeWav(t, old)

	a.refreshTakes()
	if len(a.takes) != 1 || a.takes[0].title != "Compás 1 · 0:00" {
		t.Fatalf("lista: %+v", a.takes)
	}

	a.toggleFavorite(a.takes[0])
	a.refreshTakes()
	if !a.takes[0].fav {
		t.Fatal("la favorita no se guardó")
	}

	// Rename with characters Windows does not allow; the favourite follows.
	a.renaming = old
	a.renameEd.SetText(`Solo: bueno/final?`)
	a.lastTake = old
	a.commitRename()
	want := filepath.Join(a.takesDir(), "Solo- bueno-final-.wav")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("no se renombró: %v (estado: %s)", err, a.status)
	}
	if a.lastTake != want || len(a.takes) != 1 || a.takes[0].title != "Solo- bueno-final-" || !a.takes[0].fav {
		t.Fatalf("tras renombrar: lastTake=%q takes=%+v", a.lastTake, a.takes[0])
	}
	if !strings.HasPrefix(a.takes[0].detail, "compás") {
		t.Fatalf("detalle: %q", a.takes[0].detail)
	}

	a.onlyFavs = true
	if len(a.visibleTakes()) != 1 {
		t.Fatal("filtro de favoritas")
	}

	// Sending to the Recycle Bin touches the real bin; run it on demand.
	if os.Getenv("AT_RECYCLE_TEST") == "" {
		return
	}
	a.deleteTake(a.takes[0])
	if _, err := os.Stat(want); err == nil {
		t.Fatalf("sigue existiendo: %s", a.status)
	}
	if len(a.takes) != 0 || len(a.loadMeta().Favoritas) != 0 {
		t.Fatalf("tras eliminar: takes=%d meta=%v", len(a.takes), a.loadMeta())
	}
	t.Log(a.status)
}
