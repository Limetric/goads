package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateHomebrewFormula(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to test the Homebrew formula generator")
	}

	script := filepath.ToSlash(filepath.Join("scripts", "generate-homebrew-formula.sh"))
	cmd := exec.Command("bash", script,
		"v1.2.3",
		"darwin-amd64-sha",
		"darwin-arm64-sha",
		"linux-amd64-sha",
		"linux-arm64-sha",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", script, err, out)
	}

	formula := string(out)
	wantSubstrings := []string{
		`class Goads < Formula`,
		`desc "Google Ads campaign management CLI and MCP server"`,
		`homepage "https://github.com/Limetric/goads"`,
		`version "1.2.3"`,
		`license "Apache-2.0"`,
		`url "https://github.com/Limetric/goads/releases/download/v1.2.3/goads-darwin-arm64"`,
		`sha256 "darwin-arm64-sha"`,
		`url "https://github.com/Limetric/goads/releases/download/v1.2.3/goads-darwin-amd64"`,
		`sha256 "darwin-amd64-sha"`,
		`url "https://github.com/Limetric/goads/releases/download/v1.2.3/goads-linux-arm64"`,
		`sha256 "linux-arm64-sha"`,
		`url "https://github.com/Limetric/goads/releases/download/v1.2.3/goads-linux-amd64"`,
		`sha256 "linux-amd64-sha"`,
		`binary = Dir["goads-*"].first`,
		`chmod 0755, binary`,
		`bin.install binary => "goads"`,
		`system "#{bin}/goads", "version"`,
	}

	for _, want := range wantSubstrings {
		if !strings.Contains(formula, want) {
			t.Fatalf("formula missing %q\nformula:\n%s", want, formula)
		}
	}
}

func TestHomebrewFormulaHasChangesDetectsUntrackedFormula(t *testing.T) {
	repo := initFormulaGitRepo(t)
	writeFormulaFile(t, filepath.Join(repo, "Formula", "goads.rb"), "class Goads < Formula\nend\n")

	cmd := exec.Command("bash", "scripts/homebrew-formula-has-changes.sh", repo, "Formula/goads.rb")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected untracked formula to be detected as changed: %v\n%s", err, out)
	}
}

func TestHomebrewFormulaHasChangesIgnoresCleanTrackedFormula(t *testing.T) {
	repo := initFormulaGitRepo(t)
	writeFormulaFile(t, filepath.Join(repo, "Formula", "goads.rb"), "class Goads < Formula\nend\n")
	runFormulaGit(t, repo, "add", "Formula/goads.rb")
	runFormulaGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Add formula")

	cmd := exec.Command("bash", "scripts/homebrew-formula-has-changes.sh", repo, "Formula/goads.rb")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected clean tracked formula to be detected as unchanged\n%s", out)
	}
}

func TestHomebrewFormulaHasChangesDetectsModifiedFormula(t *testing.T) {
	repo := initFormulaGitRepo(t)
	formula := filepath.Join(repo, "Formula", "goads.rb")
	writeFormulaFile(t, formula, "class Goads < Formula\nend\n")
	runFormulaGit(t, repo, "add", "Formula/goads.rb")
	runFormulaGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "Add formula")
	writeFormulaFile(t, formula, "class Goads < Formula\n  version \"1.2.3\"\nend\n")

	cmd := exec.Command("bash", "scripts/homebrew-formula-has-changes.sh", repo, "Formula/goads.rb")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected modified formula to be detected as changed: %v\n%s", err, out)
	}
}

func initFormulaGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}
	return dir
}

func runFormulaGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	cmdArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFormulaFile(t *testing.T, path, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}
