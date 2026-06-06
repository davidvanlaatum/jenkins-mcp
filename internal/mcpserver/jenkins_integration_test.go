//go:build !no_integration

package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/david/jenkins-mcp/internal/audit"
	"github.com/david/jenkins-mcp/internal/config"
	jenkinsapi "github.com/david/jenkins-mcp/internal/jenkins/api"
	"github.com/david/jenkins-mcp/internal/jenkins/model"
	"github.com/david/jenkins-mcp/internal/testutil/jenkinscontainer"
)

func TestIntegrationJenkinsMCP(t *testing.T) {
	jenkins := jenkinscontainer.Start(t)
	_, api := jenkins.Controller(t, "admin")
	freestyleBuild := jenkinscontainer.WaitForBuildResult(t, api, "example-freestyle", model.BuildResultSuccess)
	junitBuild := jenkinscontainer.WaitForBuildResult(t, api, "example-junit", model.BuildResultUnstable)
	coverageBuild := jenkinscontainer.WaitForBuildResult(t, api, "example-coverage", model.BuildResultSuccess)
	pipelineBuild := jenkinscontainer.WaitForBuildResult(t, api, "example-pipeline", model.BuildResultSuccess)
	warningsBuild := jenkinscontainer.WaitForBuildResult(t, api, "example-warnings", model.BuildResultSuccess)

	t.Run("list jobs", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		got := callIntegrationTool[struct {
			Items []struct {
				Name     string `json:"name"`
				FullName string `json:"fullName"`
			} `json:"items"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{"controller": jenkinscontainer.ControllerID})
		names := map[string]bool{}
		for _, item := range got.Items {
			names[item.Name] = true
		}
		r.Contains(names, "example-freestyle", "freestyle job")
		r.Contains(names, "example-pipeline", "pipeline job")
		r.Contains(names, "example-junit", "JUnit job")
		r.Contains(names, "example-coverage", "coverage job")
		r.Contains(names, "example-warnings", "Warnings NG job")
		r.Contains(names, "example-artifacts", "artifact job")
		r.Contains(names, "example-watch-lifecycle", "watch lifecycle job")
	})

	t.Run("job config inspection", func(t *testing.T) {
		r := require.New(t)
		adminSession, adminCleanup := connectIntegrationMCP(t, jenkins, "admin")
		defer adminCleanup()

		summary := callIntegrationTool[struct {
			Config model.JobConfig `json:"config"`
		}](t, adminSession, "jenkins_get_job_config", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-config-inspection",
		})
		r.Equal("summary", summary.Config.Mode, "summary mode")
		r.True(summary.Config.ConfigAccessible, "summary config accessible")
		r.Equal("config.xml", summary.Config.Source, "summary source")
		r.Empty(summary.Config.XML, "summary mode should not include XML")
		r.Equal("freestyle", summary.Config.Summary.Kind, "summary kind")
		r.Equal("project", summary.Config.Summary.RootElement, "summary root element")
		r.Contains(summary.Config.Summary.Description, "job config inspection", "summary description")
		r.Len(summary.Config.Summary.Parameters, 4, "summary parameters")
		assertConfigParameterRedaction(t, summary.Config.Summary.Parameters)

		xmlOnly := callIntegrationTool[struct {
			Config model.JobConfig `json:"config"`
		}](t, adminSession, "jenkins_get_job_config", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-config-inspection",
			"mode":       "xml",
			"maxBytes":   65536,
		})
		r.Equal("xml", xmlOnly.Config.Mode, "xml mode")
		r.True(xmlOnly.Config.ConfigAccessible, "xml config accessible")
		r.NotEmpty(xmlOnly.Config.XML, "xml output")
		r.False(xmlOnly.Config.Truncated, "xml output should fit")
		r.Contains(xmlOnly.Config.XML, "[REDACTED]", "xml should include redaction marker")
		for _, secret := range []string{
			"fixture-password-secret",
			"fixture-choice-secret",
			"fixture-url-secret",
			"fixture-query-secret",
			"fixture-command-secret",
		} {
			r.NotContains(xmlOnly.Config.XML, secret, "redacted XML should not expose fixture secret")
		}
		r.Contains(configWarningCodes(xmlOnly.Config.Warnings), "xml_redacted", "xml redaction warning")
		r.Contains(configWarningCodes(xmlOnly.Config.Warnings), "xml_best_effort_redaction", "xml best-effort warning")

		both := callIntegrationTool[struct {
			Config model.JobConfig `json:"config"`
		}](t, adminSession, "jenkins_get_job_config", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"mode":       "both",
			"maxBytes":   65536,
		})
		r.Equal("both", both.Config.Mode, "both mode")
		r.True(both.Config.ConfigAccessible, "pipeline config accessible")
		r.Equal("branchJob", both.Config.Summary.Kind, "pipeline config kind")
		r.Contains(both.Config.Summary.DefinitionClass, "CpsFlowDefinition", "pipeline definition class")
		r.NotEmpty(both.Config.XML, "both mode XML")
		r.NotContains(both.Config.XML, "hello from pipeline", "pipeline script should be redacted")

		noConfigSession, noConfigCleanup := connectIntegrationMCP(t, jenkins, "no-config-access")
		defer noConfigCleanup()
		fallback := callIntegrationTool[struct {
			Config model.JobConfig `json:"config"`
		}](t, noConfigSession, "jenkins_get_job_config", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-config-inspection",
			"mode":       "both",
		})
		r.Equal("both", fallback.Config.Mode, "fallback mode")
		r.False(fallback.Config.ConfigAccessible, "fallback config accessible")
		r.Equal("api/json", fallback.Config.Source, "fallback source")
		r.Empty(fallback.Config.XML, "fallback should not include XML")
		r.NotEmpty(fallback.Config.AccessError, "fallback access error")
		r.Equal("freestyle", fallback.Config.Summary.Kind, "fallback summary kind")
		r.Contains(configWarningCodes(fallback.Config.Warnings), "config_permission_denied", "fallback warning")
		assertConfigParameterRedaction(t, fallback.Config.Summary.Parameters)
	})

	t.Run("permission-specific behavior", func(t *testing.T) {
		t.Run("admin can read config and mutate", func(t *testing.T) {
			r := require.New(t)
			clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "admin", func(cfg *config.Config) {
				cfg.Mutations.Enabled = true
			})
			defer cleanup()

			config := callIntegrationTool[struct {
				Config struct {
					ConfigAccessible bool   `json:"configAccessible"`
					Source           string `json:"source"`
					XML              string `json:"xml"`
				} `json:"config"`
			}](t, clientSession, "jenkins_get_job_config", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
				"mode":       "both",
			})
			r.True(config.Config.ConfigAccessible, "admin should read config.xml")
			r.Equal("config.xml", config.Config.Source, "admin config source")
			r.Contains(config.Config.XML, "Buildable freestyle job", "admin config XML")

			trigger := callIntegrationTool[struct {
				QueueURL  string `json:"queueUrl"`
				Triggered bool   `json:"triggered"`
			}](t, clientSession, "jenkins_trigger_build", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-artifacts",
			})
			r.True(trigger.Triggered, "admin should trigger builds")
			r.NotEmpty(trigger.QueueURL, "admin trigger queue URL")
		})

		t.Run("read-only can read but cannot mutate", func(t *testing.T) {
			r := require.New(t)
			clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "read-only", func(cfg *config.Config) {
				cfg.Mutations.Enabled = true
			})
			defer cleanup()

			job := callIntegrationTool[struct {
				Job struct {
					Name string `json:"name"`
				} `json:"job"`
			}](t, clientSession, "jenkins_get_job", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
			})
			r.Equal("example-freestyle", job.Job.Name, "read-only job name")

			config := callIntegrationTool[struct {
				Config struct {
					ConfigAccessible bool `json:"configAccessible"`
					Warnings         []struct {
						Code string `json:"code"`
					} `json:"warnings"`
					Summary struct {
						Kind string `json:"kind"`
					} `json:"summary"`
				} `json:"config"`
			}](t, clientSession, "jenkins_get_job_config", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
				"mode":       "both",
			})
			r.False(config.Config.ConfigAccessible, "read-only should fall back when config.xml is not readable")
			r.Equal("freestyle", config.Config.Summary.Kind, "read-only fallback summary kind")
			r.Len(config.Config.Warnings, 1, "read-only fallback warning count")
			r.Equal("config_permission_denied", config.Config.Warnings[0].Code, "read-only fallback warning code")

			errPayload := callIntegrationToolError(t, clientSession, "jenkins_trigger_build", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
			})
			r.Equal("permission_denied", string(errPayload.Error.Code), "read-only trigger error code")
			r.NotEmpty(errPayload.Error.Message, "read-only trigger error message")
		})

		t.Run("build-only can read and trigger builds", func(t *testing.T) {
			r := require.New(t)
			clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "build-only", func(cfg *config.Config) {
				cfg.Mutations.Enabled = true
			})
			defer cleanup()

			builds := callIntegrationTool[struct {
				Items []struct {
					Number int `json:"number"`
				} `json:"items"`
			}](t, clientSession, "jenkins_list_builds", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
				"limit":      1,
			})
			r.Len(builds.Items, 1, "build-only should read builds")
			r.Equal(freestyleBuild, builds.Items[0].Number, "build-only latest build")

			trigger := callIntegrationTool[struct {
				QueueURL  string `json:"queueUrl"`
				Triggered bool   `json:"triggered"`
			}](t, clientSession, "jenkins_trigger_build", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-artifacts",
			})
			r.True(trigger.Triggered, "build-only should trigger builds")
			r.NotEmpty(trigger.QueueURL, "build-only trigger queue URL")
		})

		t.Run("no-config-access falls back safely and cannot mutate", func(t *testing.T) {
			r := require.New(t)
			clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "no-config-access", func(cfg *config.Config) {
				cfg.Mutations.Enabled = true
			})
			defer cleanup()

			config := callIntegrationTool[struct {
				Config struct {
					ConfigAccessible bool   `json:"configAccessible"`
					Source           string `json:"source"`
					XML              string `json:"xml"`
					AccessError      string `json:"accessError"`
					Warnings         []struct {
						Code   string `json:"code"`
						Detail string `json:"detail"`
					} `json:"warnings"`
					Summary struct {
						Kind        string `json:"kind"`
						Description string `json:"description"`
					} `json:"summary"`
				} `json:"config"`
			}](t, clientSession, "jenkins_get_job_config", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
				"mode":       "both",
			})
			r.False(config.Config.ConfigAccessible, "no-config-access should not read config.xml")
			r.Equal("api/json", config.Config.Source, "fallback config source")
			r.Empty(config.Config.XML, "fallback should not expose config XML")
			r.NotEmpty(config.Config.AccessError, "fallback access error")
			r.Equal("freestyle", config.Config.Summary.Kind, "fallback summary kind")
			r.Contains(config.Config.Summary.Description, "Buildable freestyle job", "fallback summary description")
			r.Len(config.Config.Warnings, 1, "fallback warning count")
			r.Equal("config_permission_denied", config.Config.Warnings[0].Code, "fallback warning code")
			r.Contains(config.Config.Warnings[0].Detail, "body omitted", "fallback warning detail")

			errPayload := callIntegrationToolError(t, clientSession, "jenkins_trigger_build", map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
			})
			r.Equal("permission_denied", string(errPayload.Error.Code), "no-config-access trigger error code")
			r.NotEmpty(errPayload.Error.Message, "no-config-access trigger error message")
		})
	})

	t.Run("queue trigger and watch lifecycle", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "build-only", func(cfg *config.Config) {
			cfg.Mutations.Enabled = true
			cfg.Watch.PollIntervalMs = 250
			cfg.Watch.DefaultWaitTimeoutMs = 5000
			cfg.Watch.MaxWaitTimeoutMs = 20000
		})
		defer cleanup()

		trigger := callIntegrationTool[struct {
			QueueURL  string `json:"queueUrl"`
			Triggered bool   `json:"triggered"`
		}](t, clientSession, "jenkins_trigger_build", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-watch-lifecycle",
		})
		r.True(trigger.Triggered, "build should be accepted")
		queueID := queueIDFromLocation(t, trigger.QueueURL)

		queue := callIntegrationTool[struct {
			Items []struct {
				ID       int64  `json:"id"`
				TaskName string `json:"taskName"`
			} `json:"items"`
		}](t, clientSession, "jenkins_list_queue", map[string]any{"controller": jenkinscontainer.ControllerID})
		r.True(queueContains(queue.Items, queueID), "list queue should include triggered item %d", queueID)

		item := callIntegrationTool[struct {
			Item struct {
				ID       int64  `json:"id"`
				TaskName string `json:"taskName"`
			} `json:"item"`
		}](t, clientSession, "jenkins_get_queue_item", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"id":         queueID,
		})
		r.Equal(queueID, item.Item.ID, "queue item id")
		r.Equal("example-watch-lifecycle", item.Item.TaskName, "queue item task")

		bootstrap := callIntegrationTool[queueWatchToolResult](t, clientSession, "jenkins_watch_queue_item", map[string]any{
			"controller":    jenkinscontainer.ControllerID,
			"id":            queueID,
			"waitTimeoutMs": 1000,
		})
		r.NotEmpty(bootstrap.Watch.State, "queue watch state")
		r.Equal("queued", bootstrap.Watch.Status, "initial queue watch status")
		r.False(bootstrap.Watch.Terminal, "queued item should not be terminal")

		executable := watchQueueUntilTerminal(t, clientSession, queueID, bootstrap.Watch.State, 15000)
		r.Equal("executable", executable.Watch.Status, "queue item should resolve to an executable")
		r.True(executable.Watch.Terminal, "executable queue item should be terminal")
		r.NotNil(executable.Watch.Build, "queue watch build handoff")
		r.Equal("example-watch-lifecycle", executable.Watch.Build.Job, "handoff build job")
		r.Greater(executable.Watch.Build.Build, 0, "handoff build number")

		build := callIntegrationTool[buildWatchToolResult](t, clientSession, "jenkins_watch_build", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        executable.Watch.Build.Job,
			"build":      executable.Watch.Build.Build,
		})
		r.Equal(executable.Watch.Build.Build, build.Watch.Build.Number, "watch build number")
		r.NotEmpty(build.Watch.State, "build watch state")
		if !build.Watch.Complete {
			build = callIntegrationTool[buildWatchToolResult](t, clientSession, "jenkins_watch_build", map[string]any{
				"controller":    jenkinscontainer.ControllerID,
				"job":           executable.Watch.Build.Job,
				"build":         executable.Watch.Build.Build,
				"lastState":     build.Watch.State,
				"waitTimeoutMs": 10000,
			})
		}
		r.True(build.Watch.Complete, "build should complete")
		r.False(build.Watch.TimedOut, "build watch should return on completion")
		r.Equal(model.BuildResultSuccess, build.Watch.Build.Result, "build result")

		cancelTrigger := callIntegrationTool[struct {
			QueueURL  string `json:"queueUrl"`
			Triggered bool   `json:"triggered"`
		}](t, clientSession, "jenkins_trigger_build", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-watch-lifecycle",
		})
		r.True(cancelTrigger.Triggered, "second build should be accepted")
		cancelQueueID := queueIDFromLocation(t, cancelTrigger.QueueURL)
		cancelBootstrap := callIntegrationTool[queueWatchToolResult](t, clientSession, "jenkins_watch_queue_item", map[string]any{
			"controller":    jenkinscontainer.ControllerID,
			"id":            cancelQueueID,
			"waitTimeoutMs": 1000,
		})
		r.Equal("queued", cancelBootstrap.Watch.Status, "cancellation fixture should start queued")

		cancelled := callIntegrationTool[struct {
			Cancelled bool `json:"cancelled"`
		}](t, clientSession, "jenkins_cancel_queue_item", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"id":         cancelQueueID,
		})
		r.True(cancelled.Cancelled, "queue cancellation should be accepted")

		cancelWatch := watchQueueUntilTerminal(t, clientSession, cancelQueueID, cancelBootstrap.Watch.State, 5000)
		r.True(cancelWatch.Watch.Terminal, "cancelled queue item should be terminal")
		r.True(cancelWatch.Watch.Cancelled || cancelWatch.Watch.Disappeared, "cancelled item should be cancelled or disappear from Jenkins queue API")
	})

	t.Run("junit test report", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		broadReport := callIntegrationTool[struct {
			Report struct {
				FailureDetailsIncluded bool `json:"failureDetailsIncluded"`
				Suites                 []struct {
					Cases []map[string]any `json:"cases"`
				} `json:"suites"`
			} `json:"report"`
		}](t, clientSession, "jenkins_get_test_report", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-junit",
			"build":      junitBuild,
			"status":     "FAILED",
			"limit":      1,
		})
		r.False(broadReport.Report.FailureDetailsIncluded, "broad report should omit failure details")
		r.Len(broadReport.Report.Suites, 1, "broad suite count")
		r.Len(broadReport.Report.Suites[0].Cases, 1, "broad failed case count")
		_, hasErrorDetails := broadReport.Report.Suites[0].Cases[0]["errorDetails"]
		r.False(hasErrorDetails, "broad report should not include errorDetails")
		_, hasErrorStackTrace := broadReport.Report.Suites[0].Cases[0]["errorStackTrace"]
		r.False(hasErrorStackTrace, "broad report should not include errorStackTrace")

		report := callIntegrationTool[struct {
			Report struct {
				TotalCount             int  `json:"totalCount"`
				FailCount              int  `json:"failCount"`
				PassCount              int  `json:"passCount"`
				FailureDetailsIncluded bool `json:"failureDetailsIncluded"`
				Suites                 []struct {
					Name  string `json:"name"`
					Cases []struct {
						ClassName    string `json:"className"`
						Name         string `json:"name"`
						Status       string `json:"status"`
						ErrorDetails string `json:"errorDetails"`
					} `json:"cases"`
				} `json:"suites"`
				Truncated bool `json:"truncated"`
			} `json:"report"`
		}](t, clientSession, "jenkins_get_test_report", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-junit",
			"build":      junitBuild,
			"status":     "FAILED",
			"className":  "example.JUnitTest",
			"caseName":   "fails",
			"limit":      1,
		})
		r.Equal(3, report.Report.TotalCount, "total test count")
		r.Equal(2, report.Report.FailCount, "failed test count")
		r.Equal(1, report.Report.PassCount, "passing test count")
		r.False(report.Report.Truncated, "single failed case should fit")
		r.True(report.Report.FailureDetailsIncluded, "exact follow-up should include failure details")
		r.Len(report.Report.Suites, 1, "suite count")
		r.Len(report.Report.Suites[0].Cases, 1, "failed case count")
		failed := report.Report.Suites[0].Cases[0]
		r.Equal("example.JUnitTest", failed.ClassName, "failed class")
		r.Equal("fails", failed.Name, "failed case")
		r.Equal("FAILED", failed.Status, "failed status")
		r.Contains(failed.ErrorDetails, "intentional fixture failure", "failure details")

		rootReport := callIntegrationTool[struct {
			Report struct {
				FailureDetailsIncluded bool `json:"failureDetailsIncluded"`
				Suites                 []struct {
					Cases []struct {
						ClassName    string `json:"className"`
						Name         string `json:"name"`
						Status       string `json:"status"`
						ErrorDetails string `json:"errorDetails"`
					} `json:"cases"`
				} `json:"suites"`
			} `json:"report"`
		}](t, clientSession, "jenkins_get_test_report", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-junit",
			"build":      junitBuild,
			"status":     "FAILED",
			"className":  "CalendarRulesTest",
			"caseName":   "test should refresh seasonal cutoff date",
			"limit":      1,
		})
		r.True(rootReport.Report.FailureDetailsIncluded, "root-package exact follow-up should include failure details")
		r.Len(rootReport.Report.Suites, 1, "root-package suite count")
		r.Len(rootReport.Report.Suites[0].Cases, 1, "root-package failed case count")
		rootFailed := rootReport.Report.Suites[0].Cases[0]
		r.Equal("CalendarRulesTest", rootFailed.ClassName, "root-package failed class")
		r.Equal("test should refresh seasonal cutoff date", rootFailed.Name, "root-package failed case")
		r.Equal("FAILED", rootFailed.Status, "root-package failed status")
		r.Contains(rootFailed.ErrorDetails, "seasonal cutoff date mismatch", "root-package failure details")

		missing, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "jenkins_get_test_report",
			Arguments: map[string]any{
				"controller": jenkinscontainer.ControllerID,
				"job":        "example-freestyle",
				"build":      freestyleBuild,
			},
		})
		r.NoError(err, "CallTool() missing JUnit")
		r.True(missing.IsError, "missing JUnit report should be returned as a structured tool error")
	})

	t.Run("coverage summary", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		build := callIntegrationTool[struct {
			Build struct {
				Coverage *struct {
					Available bool `json:"available"`
					Summaries []struct {
						Source  string `json:"source"`
						Metrics []struct {
							Name       string   `json:"name"`
							Covered    *float64 `json:"covered"`
							Missed     *float64 `json:"missed"`
							Total      *float64 `json:"total"`
							Percentage *float64 `json:"percentage"`
						} `json:"metrics"`
					} `json:"summaries"`
				} `json:"coverage"`
			} `json:"build"`
		}](t, clientSession, "jenkins_get_build", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-coverage",
			"build":      coverageBuild,
		})
		r.NotNil(build.Build.Coverage, "coverage should be included when plugin data is present")
		r.True(build.Build.Coverage.Available, "coverage should be available")
		r.NotEmpty(build.Build.Coverage.Summaries, "coverage summaries")
		r.NotEmpty(build.Build.Coverage.Summaries[0].Metrics, "coverage metrics")

		var lineMetric *struct {
			Name       string   `json:"name"`
			Covered    *float64 `json:"covered"`
			Missed     *float64 `json:"missed"`
			Total      *float64 `json:"total"`
			Percentage *float64 `json:"percentage"`
		}
		for _, summary := range build.Build.Coverage.Summaries {
			for i := range summary.Metrics {
				if strings.Contains(strings.ToLower(summary.Metrics[i].Name), "line") && summary.Metrics[i].Percentage != nil {
					lineMetric = &summary.Metrics[i]
					break
				}
			}
		}
		r.NotNil(lineMetric, "line coverage percentage metric")
		r.NotNil(lineMetric.Percentage, "line percentage")
		r.Greater(*lineMetric.Percentage, float64(0), "line percentage")
		r.Less(*lineMetric.Percentage, float64(100), "line percentage")

		missing := callIntegrationTool[struct {
			Build struct {
				Coverage *struct {
					Available bool `json:"available"`
				} `json:"coverage"`
			} `json:"build"`
		}](t, clientSession, "jenkins_get_build", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
		})
		r.Nil(missing.Build.Coverage, "metricless missing coverage should be omitted")
	})

	t.Run("artifact tools", func(t *testing.T) {
		r := require.New(t)
		buildNumber := jenkinscontainer.WaitForBuildResult(t, api, "example-artifacts", model.BuildResultSuccess)

		downloadDir := t.TempDir()
		clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "read-only", func(cfg *config.Config) {
			r.False(cfg.Mutations.Enabled, "artifact downloads should not require Jenkins mutations to be enabled")
			cfg.Artifacts.DownloadDir = downloadDir
		})
		defer cleanup()

		list := callIntegrationTool[struct {
			Artifacts []struct {
				DisplayPath  string `json:"displayPath"`
				FileName     string `json:"fileName"`
				RelativePath string `json:"relativePath"`
			} `json:"artifacts"`
		}](t, clientSession, "jenkins_list_artifacts", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-artifacts",
			"build":      buildNumber,
		})
		artifactPaths := map[string]bool{}
		for _, artifact := range list.Artifacts {
			artifactPaths[artifact.RelativePath] = true
		}
		r.Contains(artifactPaths, "artifacts/report.txt", "text artifact")
		r.Contains(artifactPaths, "artifacts/nested/details.txt", "nested text artifact")
		r.Contains(artifactPaths, "artifacts/blob.bin", "binary artifact")

		text := callIntegrationTool[struct {
			Artifact struct {
				RelativePath string `json:"relativePath"`
				Text         string `json:"text"`
				Bytes        int    `json:"bytes"`
				Inline       bool   `json:"inline"`
				Truncated    bool   `json:"truncated"`
			} `json:"artifact"`
		}](t, clientSession, "jenkins_read_artifact", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"job":          "example-artifacts",
			"build":        buildNumber,
			"relativePath": "artifacts/report.txt",
			"maxBytes":     10,
		})
		r.Equal("artifacts/report.txt", text.Artifact.RelativePath, "relative path")
		r.True(text.Artifact.Inline, "text artifact should be returned inline")
		r.True(text.Artifact.Truncated, "text artifact should respect maxBytes")
		r.Equal("hello from", text.Artifact.Text, "truncated text")
		r.Equal(10, text.Artifact.Bytes, "returned bytes")

		binary := callIntegrationTool[struct {
			Artifact struct {
				RelativePath string `json:"relativePath"`
				Bytes        int    `json:"bytes"`
				Inline       bool   `json:"inline"`
				Truncated    bool   `json:"truncated"`
			} `json:"artifact"`
		}](t, clientSession, "jenkins_read_artifact", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"job":          "example-artifacts",
			"build":        buildNumber,
			"relativePath": "artifacts/blob.bin",
		})
		r.Equal("artifacts/blob.bin", binary.Artifact.RelativePath, "relative path")
		r.False(binary.Artifact.Inline, "binary artifact should not be returned inline")
		r.False(binary.Artifact.Truncated, "binary artifact should fit within default limit")
		r.Greater(binary.Artifact.Bytes, 0, "binary bytes")

		download := callIntegrationTool[struct {
			Download struct {
				Path  string `json:"path"`
				Bytes int    `json:"bytes"`
			} `json:"download"`
		}](t, clientSession, "jenkins_download_artifact", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"job":          "example-artifacts",
			"build":        buildNumber,
			"relativePath": "artifacts/nested/details.txt",
		})
		wantPath := filepath.Join(downloadDir, "example-artifacts", "artifacts", "nested", "details.txt")
		r.Equal(wantPath, download.Download.Path, "download path")
		got, err := os.ReadFile(download.Download.Path)
		r.NoError(err, "read downloaded artifact")
		r.Equal("nested artifact fixture\n", string(got), "downloaded content")
		r.Equal(len(got), download.Download.Bytes, "downloaded byte count")

		missing, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "jenkins_read_artifact",
			Arguments: map[string]any{
				"controller":   jenkinscontainer.ControllerID,
				"job":          "example-artifacts",
				"build":        buildNumber,
				"relativePath": "artifacts/missing.txt",
			},
		})
		r.NoError(err, "CallTool() missing artifact")
		r.True(missing.IsError, "missing artifact should be returned as a structured tool error")
	})

	t.Run("warnings ng issues", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		build := callIntegrationTool[struct {
			Build struct {
				WarningsNGSummary struct {
					Available bool `json:"available"`
					Tools     []struct {
						ID    string `json:"id"`
						Name  string `json:"name"`
						Total int    `json:"total"`
					} `json:"tools"`
				} `json:"warningsNgSummary"`
			} `json:"build"`
		}](t, clientSession, "jenkins_get_build", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-warnings",
			"build":      warningsBuild,
		})
		r.True(build.Build.WarningsNGSummary.Available, "warnings summary should be available")
		r.NotEmpty(build.Build.WarningsNGSummary.Tools, "warnings tools")

		toolID := build.Build.WarningsNGSummary.Tools[0].ID
		page := callIntegrationTool[struct {
			Page struct {
				Available bool `json:"available"`
				Items     []struct {
					Severity    string `json:"severity"`
					Message     string `json:"message"`
					File        string `json:"file"`
					Line        int    `json:"line"`
					Fingerprint string `json:"fingerprint"`
				} `json:"items"`
			} `json:"page"`
			HasMore bool `json:"hasMore"`
			Limit   int  `json:"limit"`
		}](t, clientSession, "jenkins_list_issues", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-warnings",
			"build":      warningsBuild,
			"tool":       toolID,
			"limit":      50,
		})
		r.True(page.Page.Available, "warnings page should be available")
		r.NotEmpty(page.Page.Items, "warnings issues")
		r.Equal(50, page.Limit, "limit")
		issue := page.Page.Items[0]
		r.NotEmpty(issue.Message, "issue message")
		r.NotEmpty(issue.File, "issue file")
		r.NotEmpty(issue.Fingerprint, "issue fingerprint")
		r.Greater(issue.Line, 0, "issue line")

		missing := callIntegrationTool[struct {
			Page struct {
				Available bool   `json:"available"`
				Message   string `json:"message"`
				Items     []struct {
					Message string `json:"message"`
				} `json:"items"`
			} `json:"page"`
			HasMore bool `json:"hasMore"`
		}](t, clientSession, "jenkins_list_issues", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
		})
		r.False(missing.Page.Available, "Warnings NG should be unavailable for freestyle job without analysis results")
		r.Empty(missing.Page.Items, "missing Warnings NG should not return issue items")
		r.NotEmpty(missing.Page.Message, "missing Warnings NG should explain unavailable data")
		r.False(missing.HasMore, "missing Warnings NG should not paginate")
	})

	t.Run("list jobs filters and empty result", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		page := callIntegrationTool[struct {
			Items      []struct{ Name string } `json:"items"`
			NextCursor string                  `json:"nextCursor"`
			HasMore    bool                    `json:"hasMore"`
			Limit      int                     `json:"limit"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{"controller": jenkinscontainer.ControllerID, "limit": 2})
		r.Len(page.Items, 2, "first page job count")
		r.True(page.HasMore, "first page should report more jobs")
		r.NotEmpty(page.NextCursor, "first page cursor")
		r.Equal(2, page.Limit, "first page limit")

		filtered := callIntegrationTool[struct {
			Items []struct {
				Name      string `json:"name"`
				FullName  string `json:"fullName"`
				Class     string `json:"class"`
				Buildable bool   `json:"buildable"`
			} `json:"items"`
			HasMore bool `json:"hasMore"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"nameContains": "pipeline",
			"type":         "pipeline",
		})
		r.Len(filtered.Items, 1, "filtered pipeline jobs")
		r.Equal("example-pipeline", filtered.Items[0].Name, "filtered pipeline job name")
		r.True(filtered.Items[0].Buildable, "filtered pipeline job buildable")
		r.False(filtered.HasMore, "single filtered result should not have more pages")

		empty := callIntegrationTool[struct {
			Items   []struct{} `json:"items"`
			HasMore bool       `json:"hasMore"`
		}](t, clientSession, "jenkins_list_jobs", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"nameContains": "no-such-integration-job",
		})
		r.Len(empty.Items, 0, "empty filtered job result")
		r.False(empty.HasMore, "empty result should not have more pages")
	})

	t.Run("job and build metadata", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		job := callIntegrationTool[struct {
			Job struct {
				Name            string `json:"name"`
				Description     string `json:"description"`
				Buildable       bool   `json:"buildable"`
				NextBuildNumber int    `json:"nextBuildNumber"`
				LastBuild       *struct {
					Number int               `json:"number"`
					Result model.BuildResult `json:"result"`
				} `json:"lastBuild"`
			} `json:"job"`
		}](t, clientSession, "jenkins_get_job", map[string]any{"controller": jenkinscontainer.ControllerID, "job": "example-freestyle"})
		r.Equal("example-freestyle", job.Job.Name, "job name")
		r.Contains(job.Job.Description, "Buildable freestyle job", "job description")
		r.True(job.Job.Buildable, "job should be buildable")
		r.NotNil(job.Job.LastBuild, "last build")
		r.Equal(freestyleBuild, job.Job.LastBuild.Number, "last build number")
		r.Equal(model.BuildResultSuccess, job.Job.LastBuild.Result, "last build result")
		r.Equal(freestyleBuild+1, job.Job.NextBuildNumber, "next build number")

		builds := callIntegrationTool[struct {
			Items []struct {
				Number   int               `json:"number"`
				Result   model.BuildResult `json:"result"`
				Building bool              `json:"building"`
				URL      string            `json:"url"`
			} `json:"items"`
			HasMore bool `json:"hasMore"`
			Limit   int  `json:"limit"`
		}](t, clientSession, "jenkins_list_builds", map[string]any{"controller": jenkinscontainer.ControllerID, "job": "example-freestyle", "limit": 1})
		r.Len(builds.Items, 1, "build summary count")
		r.Equal(1, builds.Limit, "build list limit")
		r.False(builds.HasMore, "single build should not have another page")
		r.Equal(freestyleBuild, builds.Items[0].Number, "build summary number")
		r.Equal(model.BuildResultSuccess, builds.Items[0].Result, "build summary result")
		r.False(builds.Items[0].Building, "build summary should be complete")
		r.NotEmpty(builds.Items[0].URL, "build summary URL")

		build := callIntegrationTool[struct {
			Build struct {
				Number          int               `json:"number"`
				Result          model.BuildResult `json:"result"`
				Building        bool              `json:"building"`
				FullDisplayName string            `json:"fullDisplayName"`
				Causes          []struct {
					ShortDescription string `json:"shortDescription"`
				} `json:"causes"`
			} `json:"build"`
		}](t, clientSession, "jenkins_get_build", map[string]any{"controller": jenkinscontainer.ControllerID, "job": "example-freestyle", "build": freestyleBuild})
		r.Equal(freestyleBuild, build.Build.Number, "build number")
		r.Equal(model.BuildResultSuccess, build.Build.Result, "build result")
		r.False(build.Build.Building, "build should be complete")
		r.Contains(build.Build.FullDisplayName, "example-freestyle", "build full display name")
		r.NotEmpty(build.Build.Causes, "build causes")
	})

	t.Run("console log tools", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		chunk := callIntegrationTool[struct {
			Log struct {
				Text      string `json:"text"`
				Start     int64  `json:"start"`
				NextStart int64  `json:"nextStart"`
				More      bool   `json:"more"`
				Truncated bool   `json:"truncated"`
			} `json:"log"`
		}](t, clientSession, "jenkins_get_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"maxBytes":   12,
		})
		r.Equal(int64(0), chunk.Log.Start, "log chunk start")
		r.NotEmpty(chunk.Log.Text, "log chunk text")
		r.True(chunk.Log.Truncated, "small log chunk should be truncated")
		r.Greater(chunk.Log.NextStart, int64(0), "log next start")

		search := callIntegrationTool[struct {
			Result struct {
				Query   string `json:"query"`
				Matches []struct {
					Line int    `json:"line"`
					Text string `json:"text"`
				} `json:"matches"`
				ScannedBytes int64 `json:"scannedBytes"`
			} `json:"result"`
		}](t, clientSession, "jenkins_search_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"query":      "hello from freestyle",
			"maxMatches": 1,
		})
		r.Equal("hello from freestyle", search.Result.Query, "search query")
		r.Len(search.Result.Matches, 1, "search matches")
		r.Contains(search.Result.Matches[0].Text, "hello from freestyle", "search match text")
		r.Greater(search.Result.ScannedBytes, int64(0), "search scanned bytes")

		pagedSession, pagedCleanup := connectIntegrationMCPWithConfig(t, jenkins, "read-only", func(cfg *config.Config) {
			cfg.Limits.LogChunkBytes = 12
		})
		defer pagedCleanup()
		pagedSearch := callIntegrationTool[struct {
			Result struct {
				Matches []struct {
					Text string `json:"text"`
				} `json:"matches"`
				ScannedBytes     int64 `json:"scannedBytes"`
				NextStart        int64 `json:"nextStart"`
				ScanLimitReached bool  `json:"scanLimitReached"`
			} `json:"result"`
		}](t, pagedSession, "jenkins_search_log", map[string]any{
			"controller":   jenkinscontainer.ControllerID,
			"job":          "example-freestyle",
			"build":        freestyleBuild,
			"query":        "hello from freestyle",
			"maxScanBytes": 512,
			"maxMatches":   1,
		})
		r.Len(pagedSearch.Result.Matches, 1, "paged search matches")
		r.Contains(pagedSearch.Result.Matches[0].Text, "hello from freestyle", "paged search match text")
		r.Greater(pagedSearch.Result.ScannedBytes, int64(12), "paged search should scan beyond one configured log chunk")
		r.Greater(pagedSearch.Result.NextStart, int64(12), "paged search next start")
		r.False(pagedSearch.Result.ScanLimitReached, "paged search scan limit")

		empty := callIntegrationTool[struct {
			Result struct {
				Matches []struct{} `json:"matches"`
			} `json:"result"`
		}](t, clientSession, "jenkins_search_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"query":      "no such log line",
		})
		r.Len(empty.Result.Matches, 0, "empty search matches")

		tail := callIntegrationTool[struct {
			Log struct {
				Text      string `json:"text"`
				Start     int64  `json:"start"`
				NextStart int64  `json:"nextStart"`
				Truncated bool   `json:"truncated"`
			} `json:"log"`
		}](t, clientSession, "jenkins_tail_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-freestyle",
			"build":      freestyleBuild,
			"bytes":      64,
		})
		r.NotEmpty(tail.Log.Text, "tail log text")
		r.GreaterOrEqual(tail.Log.NextStart, tail.Log.Start, "tail log offsets")
		r.LessOrEqual(len(tail.Log.Text), 64, "tail log should honor requested byte bound")
	})

	t.Run("pipeline stage and node log tools", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCP(t, jenkins, "read-only")
		defer cleanup()

		run := callIntegrationTool[struct {
			Run struct {
				Status model.PipelineStatus `json:"status"`
				Stages []struct {
					ID     string               `json:"id"`
					Name   string               `json:"name"`
					Status model.PipelineStatus `json:"status"`
				} `json:"stages"`
			} `json:"run"`
		}](t, clientSession, "jenkins_get_pipeline_run", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      pipelineBuild,
		})
		r.Equal(model.PipelineStatusSuccess, run.Run.Status, "pipeline run status")
		r.NotEmpty(run.Run.Stages, "pipeline stages")

		stageID := ""
		for _, stage := range run.Run.Stages {
			if stage.Name == "build" {
				stageID = stage.ID
				r.Equal(model.PipelineStatusSuccess, stage.Status, "pipeline build stage status")
				break
			}
		}
		r.NotEmpty(stageID, "pipeline build stage id")

		stage := callIntegrationTool[struct {
			Stage struct {
				ID     string               `json:"id"`
				Name   string               `json:"name"`
				Status model.PipelineStatus `json:"status"`
				Nodes  []struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					HasLog bool   `json:"hasLog"`
				} `json:"nodes"`
			} `json:"stage"`
		}](t, clientSession, "jenkins_get_pipeline_stage", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      pipelineBuild,
			"stageId":    stageID,
		})
		r.Equal(stageID, stage.Stage.ID, "pipeline stage id")
		r.Equal("build", stage.Stage.Name, "pipeline stage name")
		r.Equal(model.PipelineStatusSuccess, stage.Stage.Status, "pipeline stage status")

		nodeID := ""
		for _, node := range stage.Stage.Nodes {
			if node.HasLog && strings.Contains(strings.ToLower(node.Name), "echo") {
				nodeID = node.ID
				break
			}
			if node.HasLog && nodeID == "" {
				nodeID = node.ID
			}
		}
		r.NotEmpty(nodeID, "pipeline node with log")

		nodeLog := callIntegrationTool[struct {
			Log struct {
				NodeID    string `json:"nodeId"`
				Text      string `json:"text"`
				Truncated bool   `json:"truncated"`
			} `json:"log"`
		}](t, clientSession, "jenkins_get_pipeline_node_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      pipelineBuild,
			"nodeId":     nodeID,
			"maxBytes":   128,
		})
		r.Equal(nodeID, nodeLog.Log.NodeID, "pipeline node log id")
		r.Contains(nodeLog.Log.Text, "hello from pipeline", "pipeline node log text")
		r.False(nodeLog.Log.Truncated, "pipeline node log should fit in requested bytes")
	})

	t.Run("pipeline replay tools", func(t *testing.T) {
		r := require.New(t)
		clientSession, cleanup := connectIntegrationMCPWithConfig(t, jenkins, "admin", func(cfg *config.Config) {
			cfg.Mutations.Enabled = true
		})
		defer cleanup()

		scripts := callIntegrationTool[struct {
			Scripts struct {
				SourceBuild struct {
					Job   string `json:"job"`
					Build int    `json:"build"`
				} `json:"sourceBuild"`
				Scripts []struct {
					ID        string `json:"id"`
					Kind      string `json:"kind"`
					Content   string `json:"content"`
					SizeBytes int64  `json:"sizeBytes"`
					Truncated bool   `json:"truncated"`
					SHA256    string `json:"sha256"`
				} `json:"scripts"`
				Truncated  bool  `json:"truncated"`
				TotalBytes int64 `json:"totalBytes"`
			} `json:"scripts"`
		}](t, clientSession, "jenkins_get_replay_scripts", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      pipelineBuild,
		})
		r.Equal("example-pipeline", scripts.Scripts.SourceBuild.Job, "replay source job")
		r.Equal(pipelineBuild, scripts.Scripts.SourceBuild.Build, "replay source build")
		r.False(scripts.Scripts.Truncated, "full replay script response should not be truncated")
		r.Greater(scripts.Scripts.TotalBytes, int64(0), "replay script total bytes")

		mainScript := ""
		for _, script := range scripts.Scripts.Scripts {
			if script.ID == "main" && script.Kind == "main" {
				mainScript = script.Content
				r.Greater(script.SizeBytes, int64(0), "main script size")
				r.NotEmpty(script.SHA256, "main script digest")
				r.False(script.Truncated, "main script should not be truncated")
				break
			}
		}
		r.Contains(mainScript, "hello from pipeline", "native replay should expose the original Pipeline script")
		replayedScript := strings.Replace(mainScript, "hello from pipeline", "hello from replay integration", 1)
		r.NotEqual(mainScript, replayedScript, "replay script override should change the script")

		replay := callIntegrationTool[struct {
			Replay struct {
				SourceBuild struct {
					Job   string `json:"job"`
					Build int    `json:"build"`
				} `json:"sourceBuild"`
				ScheduledBuild *struct {
					Job   string `json:"job"`
					Build int    `json:"build"`
				} `json:"scheduledBuild"`
				Replayed             bool     `json:"replayed"`
				UsedOriginalScripts  bool     `json:"usedOriginalScripts"`
				MainScriptOverridden bool     `json:"mainScriptOverridden"`
				IncludedScriptIDs    []string `json:"includedScriptIds"`
			} `json:"replay"`
		}](t, clientSession, "jenkins_replay_build", map[string]any{
			"controller":         jenkinscontainer.ControllerID,
			"job":                "example-pipeline",
			"build":              pipelineBuild,
			"mainScriptOverride": replayedScript,
		})
		r.True(replay.Replay.Replayed, "replay should be accepted")
		r.False(replay.Replay.UsedOriginalScripts, "replay should use the edited script")
		r.True(replay.Replay.MainScriptOverridden, "main script should be marked overridden")
		r.Equal("example-pipeline", replay.Replay.SourceBuild.Job, "replay source job")
		r.Equal(pipelineBuild, replay.Replay.SourceBuild.Build, "replay source build")
		r.NotNil(replay.Replay.ScheduledBuild, "replay should report predicted scheduled build")
		r.Equal("example-pipeline", replay.Replay.ScheduledBuild.Job, "scheduled build job")
		r.Greater(replay.Replay.ScheduledBuild.Build, pipelineBuild, "scheduled build number")
		r.Contains(replay.Replay.IncludedScriptIDs, "main", "submitted script ids should include main")

		jenkinscontainer.WaitForBuildNumberResult(t, api, "example-pipeline", replay.Replay.ScheduledBuild.Build, model.BuildResultSuccess)
		replayedLog := callIntegrationTool[struct {
			Log struct {
				Text string `json:"text"`
			} `json:"log"`
		}](t, clientSession, "jenkins_get_log", map[string]any{
			"controller": jenkinscontainer.ControllerID,
			"job":        "example-pipeline",
			"build":      replay.Replay.ScheduledBuild.Build,
			"maxBytes":   8192,
		})
		r.Contains(replayedLog.Log.Text, "hello from replay integration", "replayed build should run the edited script")
		r.NotContains(replayedLog.Log.Text, "hello from pipeline", "replayed build should not use the original echo")
	})
}

func callIntegrationTool[T any](t *testing.T, clientSession *mcp.ClientSession, name string, args map[string]any) T {
	t.Helper()

	r := require.New(t)
	result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	r.NoError(err, "CallTool(%s)", name)
	if result.IsError {
		r.Failf("CallTool("+name+") IsError", "%s", integrationToolErrorText(result))
	}
	r.NotNil(result.StructuredContent, "CallTool(%s) structured content", name)

	var got T
	payload, err := json.Marshal(result.StructuredContent)
	r.NoError(err, "marshal %s structured content", name)
	r.NoError(json.Unmarshal(payload, &got), "unmarshal %s structured content", name)
	return got
}

func callIntegrationToolError(t *testing.T, clientSession *mcp.ClientSession, name string, args map[string]any) toolErrorResponse {
	t.Helper()

	r := require.New(t)
	result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	r.NoError(err, "CallTool(%s)", name)
	r.True(result.IsError, "CallTool(%s) IsError", name)
	r.Nil(result.StructuredContent, "CallTool(%s) structured content", name)
	r.Len(result.Content, 1, "CallTool(%s) content", name)
	textContent, ok := result.Content[0].(*mcp.TextContent)
	r.True(ok, "CallTool(%s) content type", name)

	var payload toolErrorResponse
	r.NoError(json.Unmarshal([]byte(textContent.Text), &payload), "unmarshal %s structured error", name)
	r.NotEmpty(payload.Error.Code, "CallTool(%s) error code", name)
	r.NotEmpty(payload.Error.Message, "CallTool(%s) error message", name)
	return payload
}

func integrationToolErrorText(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func assertConfigParameterRedaction(t *testing.T, parameters []model.ParameterDefinition) {
	t.Helper()

	r := require.New(t)
	byName := map[string]model.ParameterDefinition{}
	for _, parameter := range parameters {
		byName[parameter.Name] = parameter
	}
	r.Contains(byName, "BRANCH", "branch parameter")
	if byName["BRANCH"].Default != nil {
		r.Equal("main", byName["BRANCH"].Default, "branch default")
	}
	r.Contains(byName, "DEPLOY_PASSWORD", "password parameter")
	if byName["DEPLOY_PASSWORD"].Default != nil {
		r.Equal("[REDACTED]", byName["DEPLOY_PASSWORD"].Default, "password default")
	}
	r.Empty(byName["DEPLOY_PASSWORD"].Choices, "password choices")
	r.Contains(byName, "API_TOKEN", "token parameter")
	if byName["API_TOKEN"].Default != nil {
		r.Equal("[REDACTED]", byName["API_TOKEN"].Default, "token default")
	}
	r.Empty(byName["API_TOKEN"].Choices, "token choices")
	r.Contains(byName, "REPO_URL", "URL parameter")
	if byName["REPO_URL"].Default != nil {
		defaultValue := fmt.Sprint(byName["REPO_URL"].Default)
		r.Contains(defaultValue, "example.com/acme/app.git", "URL default repository")
		r.Contains(defaultValue, "branch=main", "URL default safe query")
		r.NotContains(defaultValue, "fixture-url-secret", "URL default userinfo")
		r.NotContains(defaultValue, "fixture-query-secret", "URL default query token")
	}
}

func configWarningCodes(warnings []model.ConfigWarning) []string {
	codes := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		codes = append(codes, warning.Code)
	}
	return codes
}

func watchQueueUntilTerminal(t *testing.T, clientSession *mcp.ClientSession, id int64, state string, waitTimeoutMs int64) queueWatchToolResult {
	t.Helper()

	r := require.New(t)
	for range 30 {
		watch := callIntegrationTool[queueWatchToolResult](t, clientSession, "jenkins_watch_queue_item", map[string]any{
			"controller":    jenkinscontainer.ControllerID,
			"id":            id,
			"lastState":     state,
			"waitTimeoutMs": waitTimeoutMs,
		})
		r.NotEmpty(watch.Watch.State, "queue watch state")
		if watch.Watch.Terminal {
			return watch
		}
		r.False(watch.Watch.TimedOut, "queue watch should make progress before terminal state")
		state = watch.Watch.State
	}
	r.FailNow("queue watch did not reach a terminal state")
	return queueWatchToolResult{}
}

type queueWatchToolResult struct {
	Watch struct {
		State       string `json:"state"`
		Status      string `json:"status"`
		Terminal    bool   `json:"terminal"`
		TimedOut    bool   `json:"timedOut"`
		Cancelled   bool   `json:"cancelled"`
		Disappeared bool   `json:"disappeared"`
		Build       *struct {
			Controller string `json:"controller"`
			Job        string `json:"job"`
			Build      int    `json:"build"`
			URL        string `json:"url"`
		} `json:"build"`
	} `json:"watch"`
}

type buildWatchToolResult struct {
	Watch struct {
		State string `json:"state"`
		Build struct {
			Number int               `json:"number"`
			Result model.BuildResult `json:"result"`
		} `json:"build"`
		Complete bool `json:"complete"`
		TimedOut bool `json:"timedOut"`
	} `json:"watch"`
}

func queueIDFromLocation(t *testing.T, location string) int64 {
	t.Helper()

	r := require.New(t)
	r.NotEmpty(location, "trigger build queue location")
	u, err := url.Parse(location)
	r.NoError(err, "parse queue location")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	r.GreaterOrEqual(len(parts), 3, "queue location path")
	r.Equal("queue", parts[len(parts)-3], "queue location path")
	r.Equal("item", parts[len(parts)-2], "queue location path")
	id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	r.NoError(err, "parse queue id")
	r.Greater(id, int64(0), "queue id")
	return id
}

func queueContains(items []struct {
	ID       int64  `json:"id"`
	TaskName string `json:"taskName"`
}, id int64) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func connectIntegrationMCP(t *testing.T, jenkins jenkinscontainer.Fixture, user string) (*mcp.ClientSession, func()) {
	t.Helper()

	return connectIntegrationMCPWithConfig(t, jenkins, user, nil)
}

func connectIntegrationMCPWithConfig(t *testing.T, jenkins jenkinscontainer.Fixture, user string, mutate func(*config.Config)) (*mcp.ClientSession, func()) {
	t.Helper()

	r := require.New(t)
	cfg, api := jenkins.Controller(t, user)
	if mutate != nil {
		mutate(&cfg)
	}
	server := New(Dependencies{
		Config:  cfg,
		Jenkins: map[string]*jenkinsapi.API{jenkinscontainer.ControllerID: api},
		Audit:   &audit.Logger{},
		Version: "test",
	}).Raw()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx := t.Context()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	r.NoError(err, "server connect")
	clientSession, err := mcpClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		_ = serverSession.Close()
	}
	r.NoError(err, "client connect")

	cleanup := func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	}
	return clientSession, cleanup
}
