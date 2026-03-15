package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Agent struct {
	Name     string   `yaml:"name"`
	Schedule string   `yaml:"schedule"`
	Model    string   `yaml:"model"`
	Mode     string   `yaml:"permission_mode"`
	MaxTurns int      `yaml:"max_turns"`
	Tools    []string `yaml:"tools"`
	Budget   float64  `yaml:"budget"`
	MCPConfig string  `yaml:"mcp_config"`

	// Parsed from file
	Prompt   string `yaml:"-"`
	FilePath string `yaml:"-"`
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
	if agent.Name == "" {
		agent.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if agent.Model == "" {
		agent.Model = "sonnet"
	}
	if agent.Mode == "" {
		agent.Mode = "default"
	}

	return agent, nil
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
