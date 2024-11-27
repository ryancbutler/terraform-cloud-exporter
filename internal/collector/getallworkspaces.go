package collector

import (
	"context"
	"fmt"
	"log"

	tfe "github.com/hashicorp/go-tfe"
)

func GetAllWorkspaces(client *tfe.Client, organization string) {
	ctx := context.Background()
	options := tfe.WorkspaceListOptions{
		ListOptions: tfe.ListOptions{
			PageSize: 100,
		},
	}

	for {
		workspaces, err := client.Workspaces.List(ctx, organization, options)
		if err != nil {
			log.Fatalf("Error listing workspaces: %v", err)
		}

		for _, workspace := range workspaces.Items {
			fmt.Printf("Workspace: %s\n", workspace.Name)
		}

		if workspaces.Pagination.NextPage == 0 {
			break
		}

		options.PageNumber = workspaces.Pagination.NextPage
	}
}
