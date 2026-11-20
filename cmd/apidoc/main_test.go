package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every route must be documented and the collection must be written.
func TestBrunoCollectionCoversEveryRoute(t *testing.T) {
	reqs, err := build()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := writeBruno(dir, reqs); err != nil {
		t.Fatal(err)
	}
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if strings.HasSuffix(p, ".bru") && d.Name() != "folder.bru" && d.Name() != "collection.bru" && !strings.Contains(p, "environments") {
			n++
		}
		return nil
	})
	if n != len(reqs) {
		t.Fatalf("%d request file untuk %d route", n, len(reqs))
	}
	for _, f := range []string{"bruno.json", "collection.bru", "environments/Local.bru", "fixtures/contoh.pdf", "fixtures/contoh.png"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSampleDatesStayConsistent(t *testing.T) {
	if sampleDate("issuance_date") >= sampleDate("maturity_date") || sampleDate("birthdate") >= sampleDate("start_working_date") {
		t.Fatal("tanggal contoh tidak berurutan")
	}
}
