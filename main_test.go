package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLooksLikeFilesystemPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{name: "empty", path: "", want: false},
		{name: "relative-name", path: "05275月集体生日会", want: false},
		{name: "relative-nested", path: `foo\bar`, want: false},
		{name: "windows-drive", path: `C:\Users\demo\Pictures`, want: true},
		{name: "windows-drive-relative", path: `C:Users\demo\Pictures`, want: false},
		{name: "unc", path: `\\server\share\album`, want: true},
	}

	if runtime.GOOS == "windows" {
		cases = append(cases, struct {
			name string
			path string
			want bool
		}{name: "current-platform-abs", path: `D:\code\heic2jpg`, want: true})
	} else {
		cases = append(cases, struct {
			name string
			path string
			want bool
		}{name: "current-platform-abs", path: `/tmp/photos`, want: true})
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeFilesystemPath(tc.path); got != tc.want {
				t.Fatalf("looksLikeFilesystemPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestStreamHEICJobsSkipsDisappearingChild(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	keep := filepath.Join(root, "keep.heic")
	if err := os.WriteFile(keep, []byte("test"), 0o644); err != nil {
		t.Fatalf("write keep.heic: %v", err)
	}

	vanish := filepath.Join(root, "vanish.heic")
	if err := os.WriteFile(vanish, []byte("test"), 0o644); err != nil {
		t.Fatalf("write vanish.heic: %v", err)
	}
	if err := os.Remove(vanish); err != nil {
		t.Fatalf("remove vanish.heic: %v", err)
	}

	var jobs []job
	if err := streamHEICJobs(root, true, func(j job) {
		jobs = append(jobs, j)
	}); err != nil {
		t.Fatalf("streamHEICJobs returned error: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].src != keep {
		t.Fatalf("expected %q, got %q", keep, jobs[0].src)
	}
}

func TestStreamHEICJobsRejectsMissingRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "missing")
	err := streamHEICJobs(root, true, func(job) {})
	if err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestWalkEntryError(t *testing.T) {
	t.Parallel()

	rootErr := errors.New("root missing")
	childErr := errors.New("child missing")

	if got := walkEntryError(`C:\root`, `C:\root`, rootErr); !errors.Is(got, rootErr) {
		t.Fatalf("expected root error to be returned, got %v", got)
	}

	if got := walkEntryError(`C:\root`, `C:\root\child.heic`, childErr); got != nil {
		t.Fatalf("expected child error to be ignored, got %v", got)
	}
}
