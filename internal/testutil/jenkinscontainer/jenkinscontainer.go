//go:build !no_integration

package jenkinscontainer

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	jenkinsclient "github.com/david/jenkins-mcp/internal/jenkins/client"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
)

//go:embed testdata/jenkins/*
var testImageFiles embed.FS

const (
	ControllerID = "integration"
	port         = "8080/tcp"
)

var users = map[string]string{
	"admin":            "admin-password",
	"read-only":        "read-only-password",
	"build-only":       "build-only-password",
	"no-config-access": "no-config-access-password",
}

var expectedJobs = []string{
	"example-freestyle",
	"example-junit",
	"example-warnings",
	"example-coverage",
	"example-artifacts",
	"example-pipeline",
}

type Fixture struct {
	BaseURL string
}

func Start(t *testing.T) Fixture {
	t.Helper()

	r := require.New(t)
	_, err := testImageFiles.ReadDir("testdata/jenkins")
	r.NoError(err, "read Jenkins integration image testdata")
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := t.Context()
	container, err := testcontainers.Run(
		ctx,
		"",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:    repoRoot(t),
			Dockerfile: "internal/testutil/jenkinscontainer/testdata/jenkins/Dockerfile",
		}),
		testcontainers.WithExposedPorts(port),
		testcontainers.WithWaitStrategy(wait.ForAll(
			wait.ForLog("Jenkins is fully up and running").WithStartupTimeout(4*time.Minute),
			wait.ForHTTP("/login").WithPort(port).WithStartupTimeout(2*time.Minute),
		).WithDeadline(6*time.Minute)),
	)
	testcontainers.CleanupContainer(t, container)
	r.NoError(err, "start Jenkins container")

	host, err := container.Host(ctx)
	r.NoError(err, "Jenkins container host")
	mappedPort, err := container.MappedPort(ctx, port)
	r.NoError(err, "Jenkins container mapped port")
	fixture := Fixture{BaseURL: fmt.Sprintf("http://%s:%s", host, mappedPort.Port())}

	_, api := fixture.Controller(t, "admin")
	waitForJobs(t, api)

	return fixture
}

func (f Fixture) Controller(t *testing.T, user string) (config.Config, *jenkinsapi.API) {
	t.Helper()

	r := require.New(t)
	token, ok := users[user]
	r.True(ok, "integration user %q is configured", user)

	cfg := config.Defaults()
	cfg.DefaultController = ControllerID
	cfg.Controllers = []config.ControllerConfig{{
		ID:       ControllerID,
		URL:      f.BaseURL,
		Username: user,
		Token:    token,
	}}
	client, err := jenkinsclient.New(cfg.Controllers[0], slog.Default())
	r.NoError(err, "Jenkins client")
	return cfg, jenkinsapi.New(ControllerID, client)
}

func waitForJobs(t *testing.T, api *jenkinsapi.API) {
	t.Helper()

	r := require.New(t)
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	var missing []string
	for time.Now().Before(deadline) {
		jobs, err := api.ListJobs(t.Context(), "")
		if err == nil {
			seen := map[string]bool{}
			for _, job := range jobs {
				seen[job.Name] = true
			}
			missing = missingJobs(seen)
			if len(missing) == 0 {
				return
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	r.NoError(lastErr, "wait for Job DSL-created jobs")
	r.Failf("wait for Job DSL-created jobs", "timed out waiting for jobs: %v", missing)
}

func WaitForBuildResult(t *testing.T, api *jenkinsapi.API, job string, result model.BuildResult) int {
	t.Helper()

	r := require.New(t)
	deadline := time.Now().Add(90 * time.Second)
	var lastSeen string
	for time.Now().Before(deadline) {
		builds, err := api.ListBuilds(t.Context(), job, 0, 1)
		if err != nil {
			lastSeen = err.Error()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if len(builds) == 0 {
			lastSeen = "no builds yet"
			time.Sleep(500 * time.Millisecond)
			continue
		}
		build := builds[0]
		lastSeen = string(build.Result)
		if !build.Building && build.Result != "" {
			r.Equal(result, build.Result, "%s integration build result", job)
			return build.Number
		}
		time.Sleep(500 * time.Millisecond)
	}
	r.Failf("wait for integration build", "timed out waiting for %s build to complete; last seen: %s", job, lastSeen)
	return 0
}

func missingJobs(seen map[string]bool) []string {
	var missing []string
	for _, job := range expectedJobs {
		if !seen[job] {
			missing = append(missing, job)
		}
	}
	return missing
}

func repoRoot(t *testing.T) string {
	t.Helper()

	r := require.New(t)
	_, file, _, ok := runtime.Caller(0)
	r.True(ok, "resolve Jenkins container fixture path")
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		r.NotEqual(dir, parent, "could not find repository root from %s", file)
		dir = parent
	}
}
