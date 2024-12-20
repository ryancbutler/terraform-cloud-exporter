package collector

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	tfe "github.com/hashicorp/go-tfe"
	"github.com/ryancbutler/terraform-cloud-exporter/internal/setup"

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
		[]string{"id", "name", "organization", "terraform_version", "created_at", "environment", "locked", "current_run", "current_run_status", "current_run_created_at", "tags", "project", "plan_duration_avg", "run_failures", "run_counts", "resource_count"}, nil,
	)
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

func getWorkspacesListPage(ctx context.Context, page int, organization string, config *setup.Config, ch chan<- prometheus.Metric) error {
	// include := []tfe.WorkspaceInclude{tfe.WorkspaceIncludeCurrentRun}
	workspacesList, err := config.Client.Workspaces.List(ctx, organization, &tfe.WorkspaceListOptions{
		ListOptions: tfe.ListOptions{
			PageSize:   pageSize,
			PageNumber: page,
		},
		// Include: include,
	})
	if err != nil {
		return fmt.Errorf("%v, (organization=%s, page=%d)", err, organization, page)
	}

	for _, w := range workspacesList.Items {
		// level.Info(config.Logger).Log("msg", "Dump Cost", w.CurrentRun.CostEstimate)
		select {
		case ch <- prometheus.MustNewConstMetric(
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
			w.Project.Name,
			w.PlanDurationAverage.String(),
			fmt.Sprintf("%d", w.RunFailures),
			fmt.Sprintf("%d", w.RunsCount),
			fmt.Sprintf("%d", w.ResourceCount),
		):
		case <-ctx.Done():
			return ctx.Err()
		}
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

			// TODO: We should be able to do some of this work in parallel.
			//       Investigate potential complications before enabling it.
			for i := 1; i <= workspacesList.Pagination.TotalPages; i++ {
				if err := getWorkspacesListPage(ctx, i, name, config, ch); err != nil {
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

func getCurrentRunCreatedAt(r *tfe.Run) string {
	if r == nil {
		return "na"
	}

	return r.CreatedAt.String()
}

// func getCostEstimate(r *tfe.Run) string {
// 	if !isNilFixed(r.CostEstimate) {
// 		return "na"
// 	}

// 	return "stupid"
// }

func getCurrentTags(r []string) string {
	if r == nil {
		return "na"
	}
	return strings.Join(r, ";")
}

// func (s *workspaces) ListTags(ctx context.Context, workspaceID string, options *WorkspaceTagListOptions) (*TagList, error) {
// 	if !validStringID(&workspaceID) {
// 		return nil, ErrInvalidWorkspaceID
// 	}

// 	u := fmt.Sprintf("workspaces/%s/relationships/tags", url.PathEscape(workspaceID))

// 	req, err := s.client.NewRequest("GET", u, options)
// 	if err != nil {
// 		return nil, err
// 	}

// 	tl := &TagList{}
// 	err = req.Do(ctx, tl)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return tl, nil
// }
