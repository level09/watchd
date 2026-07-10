package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Agent struct {
	Name          string   `yaml:"name"`
	Schedule      string   `yaml:"schedule"`
	Model         string   `yaml:"model"`
	Mode          string   `yaml:"permission_mode"`
	MaxTurns      int      `yaml:"max_turns"`
	Tools         []string `yaml:"tools"`
	Budget        float64  `yaml:"budget"`
	MCPConfig     string   `yaml:"mcp_config"`
	Memory        bool     `yaml:"memory"`
	Gate          bool     `yaml:"gate"`
	Notify        string   `yaml:"notify"`
	Goal          string   `yaml:"goal"`
	Verify        string   `yaml:"verify"`
	VerifyTimeout string   `yaml:"verify_timeout"`

	// Parsed from file
	Prompt   string `yaml:"-"`
	FilePath string `yaml:"-"`
	Hash     string `yaml:"-"`
}

func Load(path string) (*Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)
	agent, err := parse(content)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	agent.FilePath = path
	sum := sha256.Sum256(data)
	agent.Hash = hex.EncodeToString(sum[:])[:12]
	if agent.Name == "" {
		agent.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if agent.Model == "" {
		agent.Model = "sonnet"
	}
	if agent.Mode == "" {
		agent.Mode = "default"
	}
	if err := agent.validate(); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	return agent, nil
}

func (a *Agent) validate() error {
	if a.Verify != "" && a.Goal == "" {
		return fmt.Errorf("verify requires goal")
	}
	if a.VerifyTimeout != "" && a.Verify == "" {
		return fmt.Errorf("verify_timeout requires verify")
	}
	if a.VerifyTimeout != "" {
		d, err := time.ParseDuration(a.VerifyTimeout)
		if err != nil || d <= 0 {
			return fmt.Errorf("verify_timeout must be a positive duration")
		}
	}
	return nil
}

func (a *Agent) VerificationTimeout() time.Duration {
	if a.VerifyTimeout == "" {
		return 2 * time.Minute
	}
	d, _ := time.ParseDuration(a.VerifyTimeout)
	return d
}

func parse(content string) (*Agent, error) {
	// Split frontmatter from body
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("missing YAML frontmatter (must start with ---)")
	}

	parts := strings.SplitN(content[4:], "\n---", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("missing closing --- for frontmatter")
	}

	var agent Agent
	if err := yaml.Unmarshal([]byte(parts[0]), &agent); err != nil {
		return nil, fmt.Errorf("invalid frontmatter: %w", err)
	}

	agent.Prompt = strings.TrimSpace(parts[1])
	return &agent, nil
}

func Discover(dir string) ([]*Agent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var agents []*Agent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "_") || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		agent, err := Load(filepath.Join(dir, entry.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", entry.Name(), err)
			continue
		}
		agents = append(agents, agent)
	}

	return agents, nil
}

func FindByName(agents []*Agent, name string) *Agent {
	for _, a := range agents {
		if a.Name == name {
			return a
		}
	}
	return nil
}
