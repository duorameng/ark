package oci

import (
	"encoding/json"
	"testing"
)

func TestGenerateArchConfigJSONCamouflage(t *testing.T) {
	layers := []*TarLayer{
		{
			FileName:    "data.dat",
			TargetCargo: "cargo/data.dat",
			Digest:      "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		},
	}

	cfgBytes, _, _, err := GenerateArchConfigJSON("amd64", layers)
	if err != nil {
		t.Fatalf("GenerateArchConfigJSON failed: %v", err)
	}

	var cfg ImageConfig
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		t.Fatalf("Unmarshal ImageConfig failed: %v", err)
	}

	if cfg.Config.WorkingDir != "/app" {
		t.Errorf("WorkingDir = %q, want /app", cfg.Config.WorkingDir)
	}

	if len(cfg.Config.Cmd) == 0 || cfg.Config.Cmd[0] != "/app/server" {
		t.Errorf("Cmd = %v, want [/app/server]", cfg.Config.Cmd)
	}

	if len(cfg.Config.Env) == 0 {
		t.Errorf("Env is empty, expected camouflage environment variables")
	}

	if cfg.Config.Labels["org.opencontainers.image.title"] != "production-service-runtime" {
		t.Errorf("Labels title = %q, want production-service-runtime", cfg.Config.Labels["org.opencontainers.image.title"])
	}

	if _, ok := cfg.Config.ExposedPorts["8080/tcp"]; !ok {
		t.Errorf("ExposedPorts does not contain 8080/tcp: %v", cfg.Config.ExposedPorts)
	}

	if cfg.Config.StopSignal != "SIGTERM" {
		t.Errorf("StopSignal = %q, want SIGTERM", cfg.Config.StopSignal)
	}

	if len(cfg.History) != 1 || cfg.History[0].CreatedBy != "COPY --chown=app:app data.dat /app/data/" {
		t.Errorf("History = %+v, expected standard Dockerfile COPY format", cfg.History)
	}

	// 测试首层为 app/server 时的 History
	camoLayers := []*TarLayer{
		{
			FileName:    "server",
			TargetCargo: "app/server",
			Digest:      "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		},
	}
	cfgBytesCamo, _, _, err := GenerateArchConfigJSON("amd64", camoLayers)
	if err != nil {
		t.Fatalf("GenerateArchConfigJSON failed: %v", err)
	}
	var cfgCamo ImageConfig
	if err := json.Unmarshal(cfgBytesCamo, &cfgCamo); err != nil {
		t.Fatalf("Unmarshal ImageConfig failed: %v", err)
	}
	if len(cfgCamo.History) != 1 || cfgCamo.History[0].CreatedBy != "COPY server /app/server" {
		t.Errorf("Camo History = %+v, expected COPY server /app/server", cfgCamo.History)
	}
}
