package main

import (
	"os"
	"text/template"
)

const composeTemplate = `version: '3'
services:
{{- range .Nodes }}
  node{{.NodeIndex}}:
    container_name: mechain-sp-{{.NodeIndex}}
    image: "{{$.Image}}"
    ports:
      - "{{.GRPCPort}}:{{$.BasePorts.GRPCPort}}"
      - "{{.P2PPort}}:{{$.BasePorts.P2PPort}}"
      - "{{.MetricPort}}:{{$.BasePorts.MetricPort}}"
      - "{{.PprofPort}}:{{$.BasePorts.PprofPort}}"
      - "{{.ProbePort}}:{{$.BasePorts.ProbePort}}"
    volumes:
      - "{{$.VolumeBasePath}}/sp{{.NodeIndex}}:/app:Z"
    command: >
      /usr/bin/mechain-sp --config /app/config.toml
{{- end }}
`

type basePorts struct {
	GRPCPort   int
	P2PPort    int
	MetricPort int
	PprofPort  int
	ProbePort  int
}
type NodeConfig struct {
	NodeIndex int
	basePorts
}

type ComposeConfig struct {
	Nodes          []NodeConfig
	Image          string
	VolumeBasePath string
	BasePorts      basePorts
}

func main() {
	numNodes := 8

	bp := basePorts{
		GRPCPort: 9033,
		P2PPort:  9063,
	}
	bp.MetricPort = bp.GRPCPort + 367
	bp.PprofPort = bp.GRPCPort + 368
	bp.ProbePort = bp.GRPCPort + 369

	nodes := make([]NodeConfig, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i] = NodeConfig{
			NodeIndex: i,
			basePorts: basePorts{
				GRPCPort:   i + bp.GRPCPort,
				P2PPort:    i + bp.P2PPort,
				MetricPort: i*1000 + bp.MetricPort,
				PprofPort:  i*1000 + bp.PprofPort,
				ProbePort:  i*1000 + bp.ProbePort,
			},
		}
	}
	composeConfig := ComposeConfig{
		Nodes:          nodes,
		Image:          "zkmelabs/mechain-storage-provider",
		VolumeBasePath: "./deployment/dockerup/local_env",
		BasePorts:      bp,
	}

	tpl, err := template.New("docker-compose").Parse(composeTemplate)
	if err != nil {
		panic(err)
	}

	file, err := os.Create("docker-compose.yml")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	err = tpl.Execute(file, composeConfig)
	if err != nil {
		panic(err)
	}

	println("Docker Compose file generated successfully!")
}
