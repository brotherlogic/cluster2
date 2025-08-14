package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
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
	masterIP     = "192.168.68.51"
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

func createRebuildIssue(ctx context.Context, client *github.Client, body string) {
	_, _, err := client.Issues.Create(ctx, user, repo, &github.IssueRequest{
		Title: proto.String(rebuildTitle),
		Body:  proto.String(body),
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

func postComment(ctx context.Context, value int, client *github.Client, issue int, comment string) error {
	// Get the prior comment
	comments, _, err := client.Issues.ListComments(ctx, user, repo, issue, &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	})
	if err != nil {
		return err
	}
	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Unix() > comments[j].CreatedAt.Unix()
	})

	// Don't double post issues
	if len(comments) > 0 {
		if comments[0].GetBody() == comment {
			return nil
		}

		elems := strings.Split(comments[0].GetBody(), ":")
		val, err := strconv.ParseInt(elems[0], 10, 64)
		if err != nil {
			log.Fatalf("Bad parse: %v", err)
		}
		if int64(value) <= val {
			log.Printf("Skipping issue because %v is less than %v", int64(value), val)
			return nil
		}
	}

	_, _, err = client.Issues.CreateComment(ctx, user, repo, issue, &github.IssueComment{
		Body: proto.String(fmt.Sprintf("%v:%v", value, comment)),
	})
	return err
}

func closeIssue(ctx context.Context, client *github.Client, issue int) error {
	_, _, err := client.Issues.Edit(ctx, user, repo, issue, &github.IssueRequest{
		State: proto.String("closed"),
	})
	return err
}

func buildCluster(ctx context.Context, client *github.Client, issue int) error {
	err := postComment(ctx, 1, client, issue, "Building Cluster - running ansible")
	if err != nil {
		return err
	}
	// Prep the cluster
	output, err := exec.Command("ansible-galaxy", "install", "-r", "./collections/requirements.yml").CombinedOutput()
	if err != nil {
		log.Printf(string(output))
		return postComment(ctx, 2, client, issue, fmt.Sprintf("Error on cluster build: %v", err))
	}

	// Build the cluster
	output, err = exec.Command("/home/simon/p3/bin/ansible-playbook", "site.yml", "-i", "inventory/my-cluster/hosts.ini").CombinedOutput()
	if err != nil {
		log.Printf(string(output))

		if strings.Contains(string(output), "UNREACHABLE") {
			return postComment(ctx, 3, client, issue, fmt.Sprintf("Validate reachability: %v", string(output)))
		}

		return postComment(ctx, 4, client, issue, fmt.Sprintf("Error on cluster build: %v -> %v", err, string(output)))
	}
	log.Printf("Run ansible: %v", err)

	err = postComment(ctx, 5, client, issue, "Cluster build complete")
	if err != nil {
		return err
	}

	// Cluster is built, copy over the files
	err = postComment(ctx, 6, client, issue, "Copying config")
	if err != nil {
		return err
	}

	output, err = exec.Command("mkdir", "-p", "/home/simon/.kube").CombinedOutput()
	if err != nil {
		log.Printf("Erorr runnign mkdir: %v", string(output))
		return err
	}

	output, err = exec.Command("ssh", masterIP, "sudo", "chmod", "777", "/etc/rancher/k3s/k3s.yaml").CombinedOutput()
	if err != nil {
		log.Printf("Erorr runnign chmod: %v", string(output))
		return err
	}

	output, err = exec.Command("scp", fmt.Sprintf("%v:/etc/rancher/k3s/k3s.yaml", masterIP), "/home/simon/.kube/config").CombinedOutput()
	if err != nil {
		log.Printf("Erorr running scp: %v", string(output))
		return err
	}

	output, err = exec.Command("ssh", masterIP, "sudo", "chmod", "600", "/etc/rancher/k3s/k3s.yaml").CombinedOutput()
	if err != nil {
		log.Printf("Erorr running chmod back: %v", string(output))
		return err
	}

	output, err = exec.Command("sed", "-i", "s|127.0.0.1|192.168.68.222|g", "/home/simon/.kube/config").CombinedOutput()
	if err != nil {
		log.Printf("Erorr running scp: %v", string(output))
		return err
	}

	return closeIssue(ctx, client, issue)
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
			nodes = append(nodes, strings.TrimSpace(line[index+1:]))
		}
	}

	// Can we reach the cluster
	res, err := exec.Command("kubectl", "get", "nodes").CombinedOutput()
	count := 0
	if err == nil {
		for _, line := range strings.Split(string(res), "\n")[1:] {
			elems := strings.Fields(line)
			if len(elems) > 0 {
				for _, node := range nodes {
					log.Printf("Checking '%v' against '%v'", elems[0], node)
					if elems[0] == node {
						count++
					}
				}
			}
		}

		// We've seen the three principal nodes
		if count == 3 {
			return
		}
	}

	log.Printf("Read %v but %v", string(res), nodes)

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
			createRebuildIssue(ctx, client, string(res))
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
