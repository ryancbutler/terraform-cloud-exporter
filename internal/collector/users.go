package collector

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/hashicorp/go-tfe"
	"github.com/ryancbutler/terraform-cloud-exporter/internal/setup"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	// users is the Metric subsystem we use.
	usersSubsystem = "users"
)

// Metric descriptors.
var (
	usersInfo = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, usersSubsystem, "info"),
		"User information.",
		[]string{"id", "username", "email"}, nil,
	)
)

// ScrapeUsers scrapes metrics about the .
type ScrapeUsers struct{}

func init() {
	Scrapers = append(Scrapers, ScrapeUsers{})
}

// Name of the Scraper. Should be unique.
func (ScrapeUsers) Name() string {
	return usersSubsystem
}

// Help describes the role of the Scraper.
func (ScrapeUsers) Help() string {
	return "Scrape information from the Users API: https://www.terraform.io/docs/cloud/api/users.html"
}

// Version of Terraform Cloud/Enterprise API from which scraper is available.
func (ScrapeUsers) Version() string {
	return "v2"
}

func getUsersListPage(ctx context.Context, page int, organization string, config *setup.Config, ch chan<- prometheus.Metric) error {
	usersList, err := config.Client.OrganizationMemberships.List(ctx, organization, &tfe.OrganizationMembershipListOptions{
		ListOptions: tfe.ListOptions{
			PageSize:   pageSize,
			PageNumber: page,
		},
	})

	if err != nil {
		return fmt.Errorf("%v, (organization=%s, page=%d)", err, organization, page)
	}

	for _, t := range usersList.Items {
		select {
		case ch <- prometheus.MustNewConstMetric(
			usersInfo,
			prometheus.GaugeValue,
			1,
			t.User.ID,
			t.User.Username,
			t.User.Email,
		):
		case <-ctx.Done():
			return ctx.Err()
		}

	}

	return nil
}

func (ScrapeUsers) Scrape(ctx context.Context, config *setup.Config, ch chan<- prometheus.Metric) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, name := range config.Organizations {
		name := name
		g.Go(func() error {
			usersList, err := config.Client.OrganizationMemberships.List(ctx, name, &tfe.OrganizationMembershipListOptions{
				ListOptions: tfe.ListOptions{
					PageSize: pageSize,
				}},
			)

			if err != nil {
				return fmt.Errorf("%v, organization=%s", err, name)
			}

			for i := 1; i <= usersList.Pagination.TotalPages; i++ {
				if err := getUsersListPage(ctx, i, name, config, ch); err != nil {
					return err
				}
			}

			return nil
		})
	}

	return g.Wait()
}
