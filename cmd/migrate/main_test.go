package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidateAppliedMigrationChecksums(t *testing.T) {
	available := map[string]string{
		"000001.up.sql": "checksum-one",
		"000002.up.sql": "checksum-two",
	}

	tests := []struct {
		name       string
		applied    []appliedMigration
		wantLegacy []string
		wantError  string
	}{
		{
			name: "matching",
			applied: []appliedMigration{
				{version: "000001.up.sql", checksum: "checksum-one"},
			},
		},
		{
			name: "legacy null checksum",
			applied: []appliedMigration{
				{version: "000001.up.sql", checksumMissing: true},
			},
			wantLegacy: []string{"000001.up.sql"},
		},
		{
			name: "missing file",
			applied: []appliedMigration{
				{version: "000003.up.sql", checksum: "checksum-three"},
			},
			wantError: "missing from the release",
		},
		{
			name: "changed file",
			applied: []appliedMigration{
				{version: "000001.up.sql", checksum: "changed"},
			},
			wantError: "checksum mismatch",
		},
		{
			name: "empty checksum is not legacy",
			applied: []appliedMigration{
				{version: "000002.up.sql", checksum: ""},
			},
			wantError: "checksum mismatch",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			legacy, err := validateAppliedMigrationChecksums(available, test.applied)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate checksums: %v", err)
			}
			if !reflect.DeepEqual(legacy, test.wantLegacy) {
				t.Fatalf("legacy versions = %v, want %v", legacy, test.wantLegacy)
			}
		})
	}
}

func TestMigrationChecksum(t *testing.T) {
	file := filepath.Join(t.TempDir(), "000001.up.sql")
	contents := []byte("SELECT 1;\n")
	if err := os.WriteFile(file, contents, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := migrationChecksum(file)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(contents))
	if got != want {
		t.Fatalf("checksum = %q, want %q", got, want)
	}
}
