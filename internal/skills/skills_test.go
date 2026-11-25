package skills

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/davinci42/WinMoi/internal/config"
	"github.com/davinci42/WinMoi/internal/runner"
	"github.com/davinci42/WinMoi/internal/state"
)

type mockSkillsRunner struct {
	targetDir string
	dryRun    bool
	calls     []runner.Command
}

func (m *mockSkillsRunner) Run(_ context.Context, cmd runner.Command) (runner.Result, error) {
	m.calls = append(m.calls, cmd)
	if len(cmd.Args) >= 3 && cmd.Args[1] == "add" {
		spec := cmd.Args[2]
		if spec == "vercel-labs/agent-skills" {
			_ = os.MkdirAll(filepath.Join(m.targetDir, "react-best-practices"), 0o755)
		}
		if spec == "anthropics/skills@pdf" {
			_ = os.MkdirAll(filepath.Join(m.targetDir, "pdf"), 0o755)
		}
	}
	return runner.Result{}, nil
}

func (m *mockSkillsRunner) RunPowerShell(_ context.Context, _ string, _ string) (runner.Result, error) {
	return runner.Result{}, nil
}

func (m *mockSkillsRunner) DryRun() bool  { return m.dryRun }
func (m *mockSkillsRunner) Verbose() bool { return false }

func TestValidateSkillMD(t *testing.T) {
	dir := t.TempDir()
	valid := "---\nname: demo-skill\ndescription: A valid skill.\n---\n# Body\n"
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(valid), 0o644)

	meta, err := ValidateSkillMD(dir)
	if err != nil {
		t.Fatalf("expected valid SKILL.md, got %v", err)
	}
	if meta.Name != "demo-skill" {
		t.Fatalf("unexpected name: %q", meta.Name)
	}

	badDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte("---\nname: only-name\n---\n"), 0o644)
	if _, err := ValidateSkillMD(badDir); err == nil {
		t.Fatal("expected error for missing description")
	}
}

func TestSkillsApplyIdempotentAndSafePrune(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "repo")
	localSkill := filepath.Join(repoDir, "skills", "my-private-skill")
	_ = os.MkdirAll(localSkill, 0o755)
	_ = os.WriteFile(filepath.Join(localSkill, "SKILL.md"), []byte("---\nname: my-private-skill\ndescription: Private\n---\n"), 0o644)

	targetDir := filepath.Join(root, "home", ".agents", "skills")
	// Unmanaged skill directory that must NEVER be deleted by --prune.
	unmanaged := filepath.Join(targetDir, "manual-skill")
	_ = os.MkdirAll(unmanaged, 0o755)

	validate := true
	cfg := &config.LoadedConfig{
		ConfigRepoDir: repoDir,
		Vars:          map[string]string{"USERPROFILE": filepath.Join(root, "home")},
		Manifest: config.Manifest{
			Skills: config.SkillsConfig{
				LocalDir:  "skills",
				Validate:  &validate,
				Tool:      "npx skills",
				ExtraArgs: []string{"-g", "-y"},
				Upstream:  []string{"vercel-labs/agent-skills", "anthropics/skills@pdf"},
			},
		},
	}
	st := state.New()
	mr := &mockSkillsRunner{targetDir: targetDir}

	rep := Apply(context.Background(), cfg, st, mr, ApplyOptions{
		TargetDir: targetDir,
		Out:       &bytes.Buffer{},
	})
	if len(rep.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", rep.Failed)
	}
	if len(rep.LocalLinked) != 1 || len(rep.UpstreamAdded) != 2 {
		t.Fatalf("unexpected apply report: %+v", rep)
	}

	// Second run should be idempotent (0 new tool calls).
	prevCalls := len(mr.calls)
	rep2 := Apply(context.Background(), cfg, st, mr, ApplyOptions{
		TargetDir: targetDir,
		Out:       &bytes.Buffer{},
	})
	if len(mr.calls) != prevCalls || len(rep2.UpstreamSkipped) != 2 {
		t.Fatalf("expected idempotent upstream skip, calls=%d rep=%+v", len(mr.calls)-prevCalls, rep2)
	}

	// Remove anthropics/skills@pdf and prune; unmanaged dir must remain untouched.
	cfg.Manifest.Skills.Upstream = []string{"vercel-labs/agent-skills"}
	rep3 := Apply(context.Background(), cfg, st, mr, ApplyOptions{
		TargetDir: targetDir,
		Prune:     true,
		Out:       &bytes.Buffer{},
	})
	if len(rep3.UpstreamPruned) != 1 || rep3.UpstreamPruned[0] != "anthropics/skills@pdf" {
		t.Fatalf("expected pdf pruned, got %+v", rep3.UpstreamPruned)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "pdf")); !os.IsNotExist(err) {
		t.Fatalf("expected pdf dir removed")
	}
	if _, err := os.Stat(unmanaged); err != nil {
		t.Fatalf("expected unmanaged dir preserved, got %v", err)
	}
}
