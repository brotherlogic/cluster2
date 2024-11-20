package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"sort"
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

func getLabels(ctx context.Context, client *github.Client, issue int) ([]string, error) {
	r, _, err := client.Issues.ListLabelsByIssue(ctx, user, repo, issue, &github.ListOptions{})
	if err != nil {
		return nil, err
	}
	var labels []string
	for _, label := range r {
		labels = append(labels, label.GetName())
	}
	return labels, nil
}

func postComment(ctx context.Context, client *github.Client, issue int, comment string) error {
	// Get the prior comment
	comments, _, err := client.Issues.ListComments(ctx, user, repo, issue, &github.IssueListCommentsOptions{})
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Unix() < comments[j].CreatedAt.Unix()
	})

	// Don't double post issues
	if len(comments) > 0 {
		log.Printf("Last comment: %v", comments[0])
		if comments[0].GetBody() == comment {
			return nil
		}
	}

	_, _, err = client.Issues.CreateComment(ctx, user, repo, issue, &github.IssueComment{
		Body: proto.String(comment),
	})
	return err
}

func buildCluster(ctx context.Context, client *github.Client, issue int) error {
	err := postComment(ctx, client, issue, "Building Cluster - running ansible")
	if err != nil {
		return err
	}
	// Prep the cluster
	output, err := exec.Command("ansible-galaxy", "install", "-r", "./collections/requirements.yml").CombinedOutput()
	if err != nil {
		log.Printf(string(output))
		return postComment(ctx, client, issue, fmt.Sprintf("Error on cluster build: %v", err))
	}

	// Build the cluster
	output, err = exec.Command("ansible-playbook", "site.yml", "-i", "inventory/my-cluster/hosts.ini").CombinedOutput()
	if err != nil {
		log.Printf(string(output))

		if strings.Contains(string(output), "UNREACHABLE") {
			return postComment(ctx, client, issue, fmt.Sprintf("Validate reachability"))
		}

		return postComment(ctx, client, issue, fmt.Sprintf("Error on cluster build: %v", err))
	}

	err = postComment(ctx, client, issue, string(output))

	return err
}

func main() {
	/*
	** Bootstraps the cluster **
	 */

	// Read the nodes we expect to see here
	var nodes []string
	bytes, err := ioutil.ReadFile("/home/simon/cluster/inventory/my-cluster/hosts.ini")
	if err != nil {
		log.Fatalf("Bad read: %v", err)
	}

	for _, line := range strings.Split(string(bytes), "\n") {
		index := strings.Index(line, "#")
		if index > 0 {
			nodes = append(nodes, line[index:])
		}
	}

	// Can we reach the cluster
	res, err := exec.Command("kubectl", "get", "nodes").CombinedOutput()
	if err == nil {
		count := 0
		for _, line := range strings.Split(string(res), "\n")[1:] {
			elems := strings.Fields(line)
			for _, node := range nodes {
				if elems[0] == node {
					count++
				}
			}
		}

		// We've seen the three principal nodes
		if count == 3 {
			return
		}
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
		if status.Code(err) == codes.NotFound {
			createRebuildIssue(ctx, client)
			return
		}
		log.Fatalf("Bad issue build: %v", err)
	}

	// See what the labels are
	labels, err := getLabels(ctx, client, issue)
	if err != nil {
		log.Fatalf("Bad labels: %v", err)
	}
	for _, label := range labels {
		if strings.ToLower(label) == "proceed" {
			buildCluster(ctx, client, issue)
		}
	}

	fmt.Printf("Cluster setup error; issue: %v -> %v\n", issue, labels)
}
