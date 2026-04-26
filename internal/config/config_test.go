package config

import "testing"

func TestLoadFromEnvironment(t *testing.T) {
	cfg, err := Load(nil, []string{
		"JENKINS_URL=https://jenkins.example.com",
		"JENKINS_USER=alice",
		"JENKINS_TOKEN=secret",
		"JENKINS_MUTATIONS=true",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DefaultController != "default" {
		t.Fatalf("DefaultController = %q", cfg.DefaultController)
	}
	if len(cfg.Controllers) != 1 {
		t.Fatalf("controllers length = %d", len(cfg.Controllers))
	}
	controller := cfg.Controllers[0]
	if controller.URL != "https://jenkins.example.com" || controller.Username != "alice" || controller.Token != "secret" {
		t.Fatalf("controller = %+v", controller)
	}
	if !cfg.Mutations.Enabled {
		t.Fatal("mutations should be enabled")
	}
	if cfg.Redacted().Controllers[0].Token != "<redacted>" {
		t.Fatal("token was not redacted")
	}
}

func TestValidateRequiresController(t *testing.T) {
	cfg := Defaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() succeeded without controllers")
	}
}
