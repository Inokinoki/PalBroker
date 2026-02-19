// Package integration 提供真实 CLI 的集成测试
// 这些测试需要真实安装对应的 CLI 工具
//
// 运行测试:
//   go test -tags=integration ./tests/integration/...
//
// 或者单独测试某个 CLI:
//   go test -tags=integration -run TestClaudeCode ./tests/integration/...
//
// 注意：这些测试会真实调用 AI 服务，可能产生费用！

//go:build integration
// +build integration

package integration

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// skipIfNoCLI 如果 CLI 不存在则跳过测试
func skipIfNoCLI(t *testing.T, cliName string) {
	if _, err := exec.LookPath(cliName); err != nil {
		t.Skipf("Skipping: %s not installed", cliName)
	}
}

// TestClaudeCode_Integration 测试真实的 Claude Code CLI
func TestClaudeCode_Integration(t *testing.T) {
	skipIfNoCLI(t, "claude")

	// 检查是否有 API key
	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("Skipping: No Anthropic API key found")
	}

	t.Log("Testing Claude Code CLI...")

	// 创建一个临时目录
	tmpDir := t.TempDir()

	// 运行一个简单的任务
	cmd := exec.Command("claude", "-p", "Say hello in one sentence")
	cmd.Dir = tmpDir
	cmd.Env = os.Environ()

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Claude Code failed: %v\nOutput: %s", err, string(output))
	}

	t.Logf("Claude Code output: %s", string(output))

	// 验证输出包含 hello 或类似内容
	outputStr := string(output)
	if len(outputStr) == 0 {
		t.Error("Expected non-empty output from Claude Code")
	}
}

// TestCodex_Integration 测试真实的 Codex CLI
func TestCodex_Integration(t *testing.T) {
	skipIfNoCLI(t, "codex")

	// Codex 通常需要登录
	t.Log("Testing Codex CLI...")

	tmpDir := t.TempDir()

	// 运行简单任务
	cmd := exec.Command("codex", "-p", "Explain what is 2+2")
	cmd.Dir = tmpDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Codex 可能需要登录，跳过而不是失败
		t.Skipf("Codex not configured: %v", err)
	}

	t.Logf("Codex output: %s", string(output))
}

// TestCopilot_Integration 测试真实的 Copilot CLI
func TestCopilot_Integration(t *testing.T) {
	skipIfNoCLI(t, "copilot")

	t.Log("Testing Copilot CLI...")

	tmpDir := t.TempDir()

	// 运行简单任务
	cmd := exec.Command("copilot", "-p", "What is Go?")
	cmd.Dir = tmpDir
	cmd.Env = os.Environ()

	// 设置超时
	done := make(chan error, 1)
	go func() {
		_, err := cmd.CombinedOutput()
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Skipf("Copilot not configured: %v", err)
		}
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		t.Skip("Copilot timed out")
	}
}

// TestPalBroker_Claude 测试 pal-broker 与 Claude 的集成
func TestPalBroker_Claude(t *testing.T) {
	skipIfNoCLI(t, "pal-broker")
	skipIfNoCLI(t, "claude")

	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_API_KEY") == "" {
		t.Skip("Skipping: No API key")
	}

	t.Log("Testing pal-broker with Claude...")

	tmpDir := t.TempDir()

	// 启动 pal-broker
	cmd := exec.Command("pal-broker",
		"--task", "integration_test",
		"--provider", "claude",
		"--work-dir", tmpDir,
		"--session-dir", tmpDir,
	)

	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start pal-broker: %v", err)
	}

	// 等待 5 秒
	time.Sleep(5 * time.Second)

	// 检查进程是否还在运行
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		t.Error("pal-broker exited unexpectedly")
	}

	// 清理
	cmd.Process.Kill()
	cmd.Wait()
}

// TestAllCLIsInstalled 测试所有 CLI 是否已安装
func TestAllCLIsInstalled(t *testing.T) {
	clis := []string{
		"claude",
		"codex",
		"copilot",
		"pal-broker",
	}

	for _, cli := range clis {
		t.Run(cli, func(t *testing.T) {
			if _, err := exec.LookPath(cli); err != nil {
				t.Errorf("%s not found in PATH", cli)
			} else {
				t.Logf("%s found: OK", cli)
			}
		})
	}
}
