package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-github/v50/github"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	repo         = "cluster2"
	user         = "brotherlogic"
	rebuildTitle = "Request Cluster Rebuild"
)

func getIssue(ctx context.Context, client *github.Client) (int, error) {
	issues, _, err := client.Issues.ListByRepo(ctx, user, repo, &github.IssueListByRepoOptions{
		ListOptions: github.ListOptions{Page: 1},
	})
	if err != nil {
		return -1, err
	}

	for _, issue := range issues {
		if issue.GetTitle() == rebuildTitle {
			return issue.GetNumber(), nil
		}
	}

	return -1, status.Error(codes.NotFound, "Rebuild issue not found!")
}

func createRebuildIssue(ctx context.Context, client *github.Client) {
	_, _, err := client.Issues.Create(ctx, user, repo, &github.IssueRequest{
		Title: proto.String(rebuildTitle),
	})
	if err != nil {
		log.Fatalf("Unable to create issue: %v", err)
	}
}

func main() {
	/*
	** Bootstraps the cluster **
	 */

	// Can we reach the cluster
	res, err := exec.Command("kubectl", "get", "nodes").CombinedOutput()
	if err == nil {
		count := 0
		for _, line := range strings.Split(string(res), "\n")[1:] {
			elems := strings.Fields(line)
			if elems[0] == "klust1" || elems[0] == "klust2" || elems[0] == "klust3" {
				count++
			}
		}

		// We've seen the three principal nodes
		if count == 3 {
			return
		}
	} else {
		fmt.Printf("kubectl error: %v", err)
	}

	// If we can't reach the cluster, start the bootstrap process
	//
	// 1. Load the github client
	content, err := os.ReadFile("/home/simon/.pat")
	if err != nil {
		log.Fatalf("Unable to read pat file: %v", err)
	}

	ctx := context.Background()

	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: string(content)},
	)
	tc := oauth2.NewClient(ctx, ts)
	client := github.NewClient(tc)

	// Check for the issue
	issue, err := getIssue(ctx, client)
	if err != nil {
		createRebuildIssue(ctx, client)
		return
	}

	fmt.Printf("Cluster setup error; issue: %v\n", issue)
}
