// Package prompt provides interactive terminal forms using charmbracelet/huh.
package prompt

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/config"
	"github.com/northwang-lucky/ssh-tunnel-daemon/internal/daemon"
)

// SelectTunnel lets the user pick an existing tunnel or choose to create one.
// A nil return value with ok=true means the user chose to create a new tunnel.
func SelectTunnel(tunnels []config.Tunnel) (*config.Tunnel, bool, error) {
	if !isTTY() {
		return nil, false, errors.New("interactive selection requires a terminal; provide a tunnel name as argument")
	}

	var options []huh.Option[string]
	for _, t := range tunnels {
		label := fmt.Sprintf("%s (%s, %s, %s)", t.Name, t.Target, t.Mode, config.FormatPorts(t.Ports))
		options = append(options, huh.NewOption(label, t.Name))
	}
	options = append(options, huh.NewOption("[create new tunnel]", "__new__"))

	var selected string
	if err := huh.NewSelect[string]().
		Title("Choose a tunnel to start").
		Options(options...).
		Value(&selected).
		Run(); err != nil {
		return nil, false, err
	}

	if selected == "__new__" {
		return nil, true, nil
	}

	for i := range tunnels {
		if tunnels[i].Name == selected {
			return &tunnels[i], true, nil
		}
	}

	return nil, false, fmt.Errorf("selected tunnel %q not found", selected)
}

// CreateTunnel prompts the user for a new tunnel definition and whether to
// persist it to the config file.
func CreateTunnel() (*config.Tunnel, bool, error) {
	if !isTTY() {
		return nil, false, errors.New("interactive creation requires a terminal; use flags to define a tunnel")
	}

	var name, target, portsStr, mode string
	var save bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Tunnel name").Value(&name),
			huh.NewInput().Title("SSH target (e.g. user@host)").Value(&target),
			huh.NewInput().Title("Ports (comma separated)").Value(&portsStr),
			huh.NewSelect[string]().Title("Mode").Options(
				huh.NewOption("local", "local"),
				huh.NewOption("remote", "remote"),
			).Value(&mode),
			huh.NewConfirm().Title("Save to config file?").Value(&save),
		),
	)
	if err := form.Run(); err != nil {
		return nil, false, err
	}

	name = trim(name)
	target = trim(target)
	portsStr = trim(portsStr)
	mode = trim(mode)

	if name == "" || target == "" || portsStr == "" || mode == "" {
		return nil, false, errors.New("all fields are required")
	}

	ports, err := config.ParsePorts(portsStr)
	if err != nil {
		return nil, false, err
	}

	return &config.Tunnel{Name: name, Target: target, Ports: ports, Mode: mode}, save, nil
}

// MultiSelectRunning lets the user pick one or more running tunnels.
func MultiSelectRunning(running []daemon.TunnelStatus) ([]string, error) {
	if !isTTY() {
		return nil, errors.New("interactive selection requires a terminal; provide tunnel names as arguments")
	}

	if len(running) == 0 {
		return nil, errors.New("no running tunnels")
	}

	var options []huh.Option[string]
	for _, r := range running {
		label := fmt.Sprintf("%s (PID %d)", r.Name, r.PID)
		options = append(options, huh.NewOption(label, r.Name))
	}

	title := "Select tunnels to stop"

	var selected []string
	if err := huh.NewMultiSelect[string]().
		Title(title).
		Options(options...).
		Value(&selected).
		Run(); err != nil {
		return nil, err
	}

	return selected, nil
}

func isTTY() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func trim(s string) string {
	var start, end int
	for start < len(s) && s[start] == ' ' {
		start++
	}
	for end = len(s); end > start && s[end-1] == ' '; end-- {
	}
	return s[start:end]
}
