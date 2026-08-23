package agent

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

var ErrExists = errors.New("agent already exists")

type Draft struct {
	Name         string
	Goal         string
	Schedule     string
	Model        string
	Budget       float64
	Instructions string
}

func (d Draft) Render() ([]byte, error) {
	d.Schedule = strings.TrimSpace(d.Schedule)
	d.Model = strings.TrimSpace(d.Model)
	d.Instructions = strings.TrimSpace(d.Instructions)
	if !slugPattern.MatchString(d.Name) {
		return nil, fmt.Errorf("name must use lowercase letters, numbers, and hyphens")
	}
	if d.Goal != "" && !slugPattern.MatchString(d.Goal) {
		return nil, fmt.Errorf("goal must use lowercase letters, numbers, and hyphens")
	}
	if d.Schedule != "" {
		interval, err := time.ParseDuration(d.Schedule)
		if err != nil || interval <= 0 {
			return nil, fmt.Errorf("schedule must be a positive duration")
		}
	}
	if d.Budget < 0 || math.IsNaN(d.Budget) || math.IsInf(d.Budget, 0) {
		return nil, fmt.Errorf("budget must be zero or a positive finite number")
	}
	if d.Instructions == "" {
		return nil, fmt.Errorf("instructions are required")
	}
	if d.Model == "" {
		d.Model = "sonnet"
	}

	frontmatter, err := yaml.Marshal(struct {
		Name     string  `yaml:"name"`
		Goal     string  `yaml:"goal,omitempty"`
		Schedule string  `yaml:"schedule"`
		Model    string  `yaml:"model"`
		Mode     string  `yaml:"permission_mode"`
		MaxTurns int     `yaml:"max_turns"`
		Budget   float64 `yaml:"budget,omitempty"`
	}{
		Name: d.Name, Goal: d.Goal, Schedule: d.Schedule, Model: d.Model,
		Mode: "default", MaxTurns: 10, Budget: d.Budget,
	})
	if err != nil {
		return nil, err
	}
	content := "---\n" + string(frontmatter) + "---\n# " + d.Name + "\n" + d.Instructions + "\n"
	return []byte(content), nil
}

func Create(dir string, draft Draft) (string, error) {
	content, err := draft.Render()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(dir, "."+draft.Name+"-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0644); err != nil {
		temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	path := filepath.Join(dir, draft.Name+".md")
	if err := os.Link(temporaryPath, path); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%w: %s", ErrExists, path)
		}
		return "", err
	}
	return path, nil
}
