package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v73/github"
	"golang.org/x/oauth2"
)

// Use gh issue list --state "open" --repo github.ibm.com/decentralized-trust-research/scalable-committer --json body,closed,closedAt,comments,createdAt,isPinned,labels,milestone,number,state,stateReason,title,updatedAt > issues.json
// to download existing issues to a json file. Change the repo name as per the need.
type Issue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Labels    []Label    `json:"labels"`
	Comments  []Comment  `json:"comments"`
	Milestone *Milestone `json:"milestone"`
}

type Label struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type Milestone struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	DueOn       *string `json:"dueOn"`
}

type Comment struct {
	Body   string `json:"body"`
	Author User   `json:"author"`
}

type User struct {
	Login string `json:"login"`
}

func main() {
	jsonPath := flag.String("file", "", "Path to the JSON file containing the issue data array.")
	owner := flag.String("owner", "", "Owner of the target GitHub repository.")
	repo := flag.String("repo", "", "Name of the target GitHub repository.")
	flag.Parse()

	if *jsonPath == "" || *owner == "" || *repo == "" {
		log.Println("All flags (--file, --owner, --repo) are required.")
		flag.Usage()
		os.Exit(1)
	}

	githubToken := os.Getenv("GITHUB_TOKEN")
	if githubToken == "" {
		log.Fatal("GITHUB_TOKEN environment variable not set.")
	}

	client := github.NewClient(oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)))

	issue, err := os.ReadFile(*jsonPath)
	if err != nil {
		log.Fatalf("Error reading JSON file: %v", err)
	}

	var sourceIssues []Issue
	if err := json.Unmarshal(issue, &sourceIssues); err != nil {
		log.Fatalf("Error unmarshaling JSON data: %v", err)
	}
	log.Printf("Successfully parsed %d issues from the file.\n", len(sourceIssues))

	log.Println("Phase 1: Collecting unique labels and milestones")
	labels, milestones := findLablesAndMilestones(sourceIssues)

	log.Println("Phase 2: Creating labels and milestones in target repository")
	if err := createLabels(client, *owner, *repo, labels); err != nil {
		log.Fatalf("failed to create labels: %v", err)
	}

	milestoneTitleToNumber, err := createMilestones(client, *owner, *repo, milestones)
	if err != nil {
		log.Fatalf("failed to create milestones: %v", err)
	}

	log.Println("Phase 3: Creating issues and comments")
	oldToNewIssueNumbers := createIssueAndComment(client, *owner, *repo, sourceIssues, milestoneTitleToNumber)

	log.Println("Phase 4: Updating issue bodies with new links")
	updateIssueLinks(client, *owner, *repo, sourceIssues, oldToNewIssueNumbers)

	log.Println("\n All issues created and linked successfully! ---")
}

func findLablesAndMilestones(issues []Issue) (map[string]Label, map[string]Milestone) {
	uniqueLabels := make(map[string]Label)
	uniqueMilestones := make(map[string]Milestone)

	for _, issue := range issues {
		for _, label := range issue.Labels {
			uniqueLabels[label.Name] = label
		}
		if issue.Milestone != nil {
			uniqueMilestones[issue.Milestone.Title] = *issue.Milestone
		}
	}
	log.Printf("Found %d unique labels and %d unique milestones.\n", len(uniqueLabels), len(uniqueMilestones))

	return uniqueLabels, uniqueMilestones
}

func createLabels(client *github.Client, owner, repo string, labels map[string]Label) error {
	existingLabels, _, err := client.Issues.ListLabels(context.Background(), owner, repo, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch existing labels: %v", err)
	}
	existingLabelNames := make(map[string]bool)
	for _, label := range existingLabels {
		existingLabelNames[label.GetName()] = true
	}

	for name, label := range labels {
		if !existingLabelNames[name] {
			log.Printf("Creating label: [%s]", name)
			_, _, err := client.Issues.CreateLabel(context.Background(), owner, repo, &github.Label{
				Name:        &label.Name,
				Color:       &label.Color,
				Description: &label.Description,
			})
			if err != nil {
				log.Printf("Warning: failed to create label [%s]: %v\n", name, err)
			}
		}
	}

	return nil
}

func createMilestones(client *github.Client, owner, repo string, milestones map[string]Milestone) (map[string]int, error) {
	milestoneTitleToNumber := make(map[string]int)
	existingMilestones, _, err := client.Issues.ListMilestones(context.Background(), owner, repo, &github.MilestoneListOptions{State: "all"})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch existing milestones: %v", err)
	}
	for _, m := range existingMilestones {
		milestoneTitleToNumber[m.GetTitle()] = m.GetNumber()
	}

	for title, milestone := range milestones {
		if _, exists := milestoneTitleToNumber[title]; exists {
			continue
		}

		log.Printf("Creating milestone: %s", title)

		newMilestoneReq := &github.Milestone{
			Title:       &milestone.Title,
			Description: &milestone.Description,
		}

		if milestone.DueOn != nil {
			parsedTime, err := time.Parse(time.RFC3339, *milestone.DueOn)
			if err != nil {
				log.Printf("Warning: could not parse due date for milestone '%s': %v. Creating without due date.", title, err)
			} else {
				newMilestoneReq.DueOn = &github.Timestamp{Time: parsedTime}
			}
		}

		createdMilestone, _, err := client.Issues.CreateMilestone(context.Background(), owner, repo, newMilestoneReq)
		if err != nil {
			log.Printf("Warning: failed to create milestone '%s': %v\n", title, err)
		} else {
			milestoneTitleToNumber[createdMilestone.GetTitle()] = createdMilestone.GetNumber()
		}
	}

	return milestoneTitleToNumber, nil
}

func createIssueAndComment(client *github.Client, owner, repo string, issues []Issue, milestoneTitleToNum map[string]int) map[int]int {
	oldToNewIssueNumbers := make(map[int]int)
	for _, issue := range issues {
		labelNames := make([]string, 0)
		for _, label := range issue.Labels {
			labelNames = append(labelNames, label.Name)
		}

		newIssueRequest := &github.IssueRequest{
			Title:  &issue.Title,
			Body:   &issue.Body,
			Labels: &labelNames,
		}

		if issue.Milestone != nil {
			if newMilestoneNum, ok := milestoneTitleToNum[issue.Milestone.Title]; ok {
				newIssueRequest.Milestone = &newMilestoneNum
			}
		}

		log.Printf("Creating issue for: \"%s\"...", issue.Title)
		createdIssue, _, err := client.Issues.Create(context.Background(), owner, repo, newIssueRequest)
		if err != nil {
			log.Printf("Failed to create issue \"%s\": %v", issue.Title, err)
			continue
		}

		newlyCreatedNumber := createdIssue.GetNumber()
		oldToNewIssueNumbers[issue.Number] = newlyCreatedNumber

		if len(issue.Comments) > 0 {
			log.Printf("Consolidating %d comments for new issue #%d", len(issue.Comments), newlyCreatedNumber)
			var combinedComments strings.Builder
			combinedComments.WriteString("### Comments from original issue:\n\n---\n\n")

			for _, comment := range issue.Comments {
				commentHeader := fmt.Sprintf("**Comment from @%s:**\n\n", comment.Author.Login)
				combinedComments.WriteString(commentHeader)
				combinedComments.WriteString(comment.Body)
				combinedComments.WriteString("\n\n---\n\n")
			}

			if combinedComments.Len() > 0 {
				combinedBody := combinedComments.String()
				issueComment := &github.IssueComment{Body: &combinedBody}
				_, _, err := client.Issues.CreateComment(context.Background(), owner, repo, newlyCreatedNumber, issueComment)
				if err != nil {
					log.Printf("Failed to create consolidated comment for issue #%d: %v\n", newlyCreatedNumber, err)
				} else {
					log.Printf("Successfully posted consolidated comments.\n")
				}
			}
		}
	}

	return oldToNewIssueNumbers
}

func updateIssueLinks(client *github.Client, owner, repo string, issues []Issue, oldToNewIssueNumbers map[int]int) {
	issueLinkRegex := regexp.MustCompile(`#(\d+)`)

	for _, sourceIssue := range issues {
		newlyCreatedNumber, ok := oldToNewIssueNumbers[sourceIssue.Number]
		if !ok {
			log.Printf("Skipping body update for old issue #%d as it was not created.", sourceIssue.Number)
			continue
		}

		updatedBody := issueLinkRegex.ReplaceAllStringFunc(sourceIssue.Body, func(match string) string {
			oldNumStr := strings.TrimPrefix(match, "#")
			oldNum, _ := strconv.Atoi(oldNumStr)

			if newNum, found := oldToNewIssueNumbers[oldNum]; found {
				return fmt.Sprintf("#%d", newNum)
			}
			return match
		})

		if updatedBody != sourceIssue.Body {
			log.Printf("Updating body for new issue #%d (from old #%d)...", newlyCreatedNumber, sourceIssue.Number)
			updateReq := &github.IssueRequest{Body: &updatedBody}
			_, _, err := client.Issues.Edit(context.Background(), owner, repo, newlyCreatedNumber, updateReq)
			if err != nil {
				log.Printf("Failed to update body for new issue #%d: %v\n", newlyCreatedNumber, err)
			} else {
				log.Printf("Success!\n")
			}
		}
	}
}
