package collector

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/ryancbutler/terraform-cloud-exporter/internal/setup"

	"maps"

	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// workspaces is the Metric subsystem we use.
	workspacesSubsystem = "workspaces"

	// TODO: We might want to allow the user to control pageSize via cli/config
	// 		* This could be handy for users hitting API rate limits (30 per sec).
	// 		* Investigate performance of (100 requests for 1 item) vs (1 request for 100 items).
	pageSize = 100
)

// Metric descriptors.
var (
	WorkspacesInfo = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, workspacesSubsystem, "info"),
		"Information about existing workspaces",
		[]string{"ws_id", "ws_name", "organization", "terraform_version", "created_at", "environment", "locked", "current_run", "current_run_status", "current_run_created_at", "tags", "tf_environment", "aws_account_id", "project_id", "project_name"}, nil,
	)

	resourceCount    = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "resource_count"), "Total number of managed resources", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	failCount        = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "run_failure_count"), "Total number of failed runs", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	runCount         = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "run_count"), "Total number of runs", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	wsLocked         = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "locked_count"), "Workspace Locked", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	policyFailure    = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "policy_check_failures"), "Total policy failures", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	applyDuration    = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "apply_duration_seconds"), "Apply duration average", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	planDuration     = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "plan_duration_seconds"), "Plan duration average", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	costDelta        = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "run_cost_delta"), "Current run cost delta", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
	daysSinceLastRun = prometheus.NewDesc(prometheus.BuildFQName(namespace, workspacesSubsystem, "days_since_last_run"), "Number of days since last run", []string{"ws_id", "tags", "tf_environment", "aws_account_id", "ws_name", "project_id", "project_name", "tf_version", "created_at", "current_run_status", "current_run_created_at", "current_run_id", "locked", "organization"}, nil)
)

// ScrapeWorkspaces scrapes metrics about the workspaces.
type ScrapeWorkspaces struct{}

func init() {
	Scrapers = append(Scrapers, ScrapeWorkspaces{})
}

// Name of the Scraper. Should be unique.
func (ScrapeWorkspaces) Name() string {
	return workspacesSubsystem
}

// Help describes the role of the Scraper.
func (ScrapeWorkspaces) Help() string {
	return "Scrape information from the Workspaces API: https://www.terraform.io/docs/cloud/api/workspaces.html"
}

// Version of Terraform Cloud/Enterprise API from which scraper is available.
func (ScrapeWorkspaces) Version() string {
	return "v2"
}

func getWorkspacesListPage(ctx context.Context, page int, organization string, config *setup.Config, ch chan<- prometheus.Metric, projects map[string]string) error {
	// include := []tfe.WorkspaceInclude{tfe.WorkspaceIncludeCurrentRun}
	workspacesList, err := config.Client.Workspaces.List(ctx, organization, &tfe.WorkspaceListOptions{
		ListOptions: tfe.ListOptions{
			PageSize:   pageSize,
			PageNumber: page,
		},
		Include: []tfe.WSIncludeOpt{"organization", "current_run"},

		//project seems to cause a panic
		// Include: []tfe.WSIncludeOpt{
		// 	"project",
		// 	"organization",
		// 	"current_configuration_version",
		// 	"current_configuration_version.ingress_attributes",
		// 	"current_run",
		// 	"current_run.plan",
		// 	"current_run.configuration_version",
		// 	"current_run.configuration_version.ingress_attributes",
		// 	"locked_by",
		// 	"readme",
		// 	"outputs",
		// 	"current-state-version",
		// },
	})
	if err != nil {
		return fmt.Errorf("%v, (organization=%s, page=%d)", err, organization, page)
	}

	for _, w := range workspacesList.Items {

		ch <- prometheus.MustNewConstMetric(
			WorkspacesInfo,
			prometheus.GaugeValue,
			1,
			w.ID,
			w.Name,
			w.Organization.Name,
			w.TerraformVersion,
			w.CreatedAt.String(),
			w.Environment,
			fmt.Sprintf("%t", w.Locked),
			getCurrentRunID(w.CurrentRun),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Project.ID,
			projects[w.Project.ID],
		)

		ch <- prometheus.MustNewConstMetric(
			resourceCount,
			prometheus.GaugeValue,
			float64(w.ResourceCount), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)

		ch <- prometheus.MustNewConstMetric(
			failCount,
			prometheus.GaugeValue,
			float64(w.RunFailures), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			runCount,
			prometheus.GaugeValue,
			float64(w.RunsCount), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			wsLocked,
			prometheus.GaugeValue,
			convertBool(w.Locked), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			policyFailure,
			prometheus.GaugeValue,
			float64(w.PolicyCheckFailures), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			applyDuration,
			prometheus.GaugeValue,
			float64((w.ApplyDurationAverage * time.Millisecond).Seconds()), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			planDuration,
			prometheus.GaugeValue,
			float64((w.PlanDurationAverage * time.Millisecond).Seconds()), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			costDelta,
			prometheus.GaugeValue,
			getCurrentCostDelta(w.CurrentRun), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			daysSinceLastRun,
			prometheus.GaugeValue,
			getSinceLastRun(w.CurrentRun), // Main metric
			w.ID,
			getCurrentTags(w.TagNames),
			getTagValue(w.TagNames, "environment:"),
			getTagValue(w.TagNames, "aws_account_id:"),
			w.Name,
			w.Project.ID,
			projects[w.Project.ID],
			w.TerraformVersion,
			w.CreatedAt.String(),
			getCurrentRunStatus(w.CurrentRun),
			getCurrentRunCreatedAt(w.CurrentRun),
			getCurrentRunID(w.CurrentRun),
			fmt.Sprintf("%t", w.Locked),
			w.Organization.Name,
		)
	}
	return nil
}

// Scrape collects data from Terraform API and sends it over channel as prometheus metric.
func (ScrapeWorkspaces) Scrape(ctx context.Context, config *setup.Config, ch chan<- prometheus.Metric) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, name := range config.Organizations {
		name := name
		g.Go(func() error {
			// TODO: Dummy list call to get the number of workspaces.
			//       Investigate if there is a better way to get the workspace count.
			workspacesList, err := config.Client.Workspaces.List(ctx, name, &tfe.WorkspaceListOptions{
				ListOptions: tfe.ListOptions{PageSize: pageSize},
			})
			if err != nil {
				return fmt.Errorf("%v, organization=%s", err, name)
			}

			projectMap := getProjectList(ctx, name, config)

			// TODO: We should be able to do some of this work in parallel.
			//       Investigate potential complications before enabling it.
			for i := 1; i <= workspacesList.Pagination.TotalPages; i++ {
				if err := getWorkspacesListPage(ctx, i, name, config, ch, projectMap); err != nil {
					return err
				}
			}

			return nil
		})
	}

	return g.Wait()
}

func getCurrentRunID(r *tfe.Run) string {
	if r == nil {
		return "na"
	}

	return r.ID
}

func getCurrentRunStatus(r *tfe.Run) string {
	if r == nil {
		return "na"
	}

	return string(r.Status)
}

func getCurrentCostDelta(r *tfe.Run) float64 {
	if r == nil {
		return 0
	}
	newStr := strings.ReplaceAll(r.CostEstimate.DeltaMonthlyCost, "$", "")
	floatVal, err := strconv.ParseFloat(newStr, 64)
	if err != nil {
		return 0
	}
	return floatVal
}

func getCurrentRunCreatedAt(r *tfe.Run) string {
	if r == nil {
		return "na"
	}

	return r.CreatedAt.String()
}

func getSinceLastRun(r *tfe.Run) float64 {
	if r == nil {
		return 0
	}
	date := time.Now()
	diff := date.Sub(r.CreatedAt)
	days := int(diff.Hours() / 24)
	return float64(days)
}

func getCurrentTags(r []string) string {
	if len(r) == 0 {
		return "na"
	}
	return strings.Join(r, ";")
}

func getTagValue(r []string, s string) string {
	if len(r) == 0 {
		return "na"
	}

	for _, tag := range r {
		if strings.HasPrefix(tag, s) {
			return strings.TrimLeft(tag, s)
		}

	}

	return "na"

}

func convertBool(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func getProjectList(ctx context.Context, organization string, config *setup.Config) map[string]string {

	projectsList, err := config.Client.Projects.List(ctx, organization, &tfe.ProjectListOptions{
		ListOptions: tfe.ListOptions{PageSize: pageSize},
	})
	if err != nil {
		fmt.Printf("Error: %v, organization=%s\n", err, organization)
		return map[string]string{}
	}

	m := make(map[string]string)
	for i := 1; i <= projectsList.Pagination.TotalPages; i++ {
		projectsPage := getProjectsListPage(ctx, i, organization, config)
		//Copy the map to the main map
		maps.Copy(m, projectsPage)
	}

	return m
}

func getProjectsListPage(ctx context.Context, page int, organization string, config *setup.Config) map[string]string {
	projectsList, err := config.Client.Projects.List(ctx, organization, &tfe.ProjectListOptions{
		ListOptions: tfe.ListOptions{
			PageSize:   pageSize,
			PageNumber: page,
		},
	})
	if err != nil {
		fmt.Printf("Error: %v, organization=%s\n", err, organization)
	}
	m := make(map[string]string)
	for _, w := range projectsList.Items {
		m[w.ID] = w.Name
	}

	return m
}
