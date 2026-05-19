package discovery

import (
	"fmt"
	"strings"
)

type Display struct {
	Options *DisplayOptions
}

func NewDisplay() *Display {
	return &Display{
		Options: NewDisplayOptions(),
	}
}

type DisplayOptions struct {
	Quiet bool
	Wide  bool
}

func NewDisplayOptions() *DisplayOptions {
	return &DisplayOptions{
		Quiet: false,
		Wide:  false,
	}
}

func (o *DisplayOptions) WithQuiet(quiet bool) *DisplayOptions {
	o.Quiet = quiet
	return o
}

func (o *DisplayOptions) WithWide(wide bool) *DisplayOptions {
	o.Wide = wide
	return o
}

type TableBuilder struct {
	headers []string
	rows    [][]string
	widths  []int
}

func NewTableBuilder() *TableBuilder {
	return &TableBuilder{
		headers: make([]string, 0),
		rows:    make([][]string, 0),
		widths:  make([]int, 0),
	}
}

func (tb *TableBuilder) AddHeader(headers ...string) {
	tb.headers = headers
	tb.widths = make([]int, len(headers))
	for i, h := range headers {
		tb.widths[i] = len(h)
	}
}

func (tb *TableBuilder) AddRow(cells ...string) {
	row := make([]string, len(tb.headers))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		} else {
			row[i] = ""
		}
	}

	for i, cell := range row {
		if len(cell) > tb.widths[i] {
			tb.widths[i] = len(cell)
		}
	}

	tb.rows = append(tb.rows, row)
}

func (tb *TableBuilder) String() string {
	if len(tb.headers) == 0 {
		return ""
	}

	var sb strings.Builder

	for i, h := range tb.headers {
		if i > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(padRight(h, tb.widths[i]))
	}
	sb.WriteString("\n")

	for _, row := range tb.rows {
		for i, cell := range row {
			if i > 0 {
				sb.WriteString("  ")
			}
			sb.WriteString(padRight(cell, tb.widths[i]))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func (d *Display) FormatContainers(services []*ServiceInfo) string {
	if len(services) == 0 {
		return "No containers found\n"
	}

	if d.Options.Quiet {
		var sb strings.Builder
		for _, s := range services {
			sb.WriteString(s.ContainerID)
			sb.WriteString("\n")
		}
		return sb.String()
	}

	tb := NewTableBuilder()
	tb.AddHeader("NAME", "IMAGE", "STATUS", "PORTS")

	for _, s := range services {
		ports := d.FormatPortMappings(s.Ports)
		tb.AddRow(s.Name, s.Image, s.Status, ports)
	}

	return tb.String()
}

func (d *Display) FormatNetworks(networks []*NetworkInfo) string {
	if len(networks) == 0 {
		return "No networks found\n"
	}

	tb := NewTableBuilder()
	tb.AddHeader("NAME", "DRIVER", "SCOPE", "CONTAINERS")

	for _, n := range networks {
		containers := d.FormatNetworkContainers(n.Containers)
		tb.AddRow(n.Name, n.Driver, n.Scope, containers)
	}

	return tb.String()
}

func (d *Display) FormatServices(services []*ServiceInfo) string {
	if len(services) == 0 {
		return "No services found\n"
	}

	tb := NewTableBuilder()
	tb.AddHeader("NAME", "IMAGE", "STATUS", "CONNECTION")

	for _, s := range services {
		connection := SuggestConnectionString(s)
		if connection == "" {
			connection = "-"
		}
		tb.AddRow(s.Name, s.Image, s.Status, connection)
	}

	return tb.String()
}

func (d *Display) FormatDiscoveryResult(result *DiscoveryResult) string {
	var sb strings.Builder

	sb.WriteString("=== Containers ===\n")
	sb.WriteString(d.FormatContainers(result.Services))

	sb.WriteString("\n=== Networks ===\n")
	sb.WriteString(d.FormatNetworks(result.Networks))

	return sb.String()
}

func (d *Display) FormatPortMappings(ports []PortMapping) string {
	if len(ports) == 0 {
		return "-"
	}

	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = p.String()
	}

	return strings.Join(parts, ", ")
}

func (d *Display) FormatStatus(status string) string {
	return status
}

func (d *Display) FormatSource(source ServiceSource) string {
	return string(source)
}

func (d *Display) FormatNetworkContainers(containers []string) string {
	if len(containers) == 0 {
		return "-"
	}

	const maxDisplay = 5
	if len(containers) > maxDisplay {
		displayed := strings.Join(containers[:maxDisplay], ", ")
		return fmt.Sprintf("%s (+%d more)", displayed, len(containers)-maxDisplay)
	}

	return strings.Join(containers, ", ")
}

func (d *Display) FormatComposeFile(cf *ComposeFile) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Compose File: %s\n", cf.Path))
	sb.WriteString(fmt.Sprintf("Version: %s\n", cf.Version))

	sb.WriteString("\nServices:\n")
	for name, svc := range cf.Services {
		sb.WriteString(fmt.Sprintf("  - %s (%s)\n", name, svc.Image))
	}

	if len(cf.Networks) > 0 {
		sb.WriteString("\nNetworks:\n")
		for name := range cf.Networks {
			sb.WriteString(fmt.Sprintf("  - %s\n", name))
		}
	}

	if len(cf.Volumes) > 0 {
		sb.WriteString("\nVolumes:\n")
		for name := range cf.Volumes {
			sb.WriteString(fmt.Sprintf("  - %s\n", name))
		}
	}

	return sb.String()
}

func (d *Display) FormatServiceDetails(s *ServiceInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Name: %s\n", s.Name))
	sb.WriteString(fmt.Sprintf("Container ID: %s\n", s.ContainerID))
	sb.WriteString(fmt.Sprintf("Image: %s\n", s.Image))
	sb.WriteString(fmt.Sprintf("Status: %s\n", s.Status))
	sb.WriteString(fmt.Sprintf("Source: %s\n", s.Source()))

	if len(s.Networks) > 0 {
		sb.WriteString(fmt.Sprintf("Networks: %s\n", strings.Join(s.Networks, ", ")))
	}

	if len(s.Ports) > 0 {
		sb.WriteString(fmt.Sprintf("Ports: %s\n", d.FormatPortMappings(s.Ports)))
	}

	if connection := SuggestConnectionString(s); connection != "" {
		sb.WriteString(fmt.Sprintf("Connection: %s\n", connection))
	}

	return sb.String()
}

func (d *Display) FormatNetworkDetails(n *NetworkInfo) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Name: %s\n", n.Name))
	sb.WriteString(fmt.Sprintf("ID: %s\n", n.ID))
	sb.WriteString(fmt.Sprintf("Driver: %s\n", n.Driver))
	sb.WriteString(fmt.Sprintf("Scope: %s\n", n.Scope))

	if n.Internal {
		sb.WriteString("Internal: yes\n")
	}

	if len(n.Containers) > 0 {
		sb.WriteString(fmt.Sprintf("Containers: %s\n", strings.Join(n.Containers, ", ")))
	}

	return sb.String()
}
