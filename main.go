package main

import (
	"fmt"
	"github.com/google/go-github/github"
	"github.com/spf13/cobra"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

var (
	save            bool
	org             string
	repo            string
	label           string
	outputFile      string
	token           string
	previousRelease string
	currentRelease  string

	gh *GithubClient

	rootCmd = &cobra.Command{
		Use:               "changelog",
		Short:             "Changelog between GITHUB repository releases",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		Long: `
Changelog generator for GITHUB releases automatically.
`,
		PreRun: func(cmd *cobra.Command, args []string) {
			if token != "" {
				tok, err := GetAPITokenFromFile(token)
				if err != nil {
					log.Fatalf("Error accessing user supplied token_file: %v\n", err)
				}
				gh = NewGithubClient(org, tok)
			} else {
				gh = NewGithubClientNoAuth(org)
			}

		},
		Run: func(cmd *cobra.Command, args []string) {

			f, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
			if err != nil {
				log.Printf("Failed to open and/or create output file %s", outputFile)
				return
			}
			defer func() {
				if err := f.Close(); err != nil {
					log.Printf("Error during closing file %s: %s\n", outputFile, err)
				}
			}()

			log.Printf("Start fetching release note from %s/%s", org, repo)
			queries, err := createQueryString(repo)
			if err != nil {
				log.Printf("Failed to create query string for %s", repo)
				return
			}

			log.Printf("Query: %v", queries)
			issuesResult, err := gh.SearchIssues(queries, "")
			if err != nil {
				log.Printf("Failed to fetch PR with release note for %s: %s", repo, err)
				return
			}
			content := groupedLabelContent(issuesResult)

			log.Printf("Saving data on: %v", outputFile)
			f.WriteString(content)

			if save {
				log.Printf("Update GITHUB release notes")
				if err := gh.UpdateReleaseNotes(repo, currentRelease, content); err != nil {
					log.Printf("Error updating release notes: %s", err)
				}
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&save, "save", "s", false, "Save release notes on Github")
	rootCmd.PersistentFlags().StringVarP(&org, "user", "u", "knabben", "Github owner or org")
	rootCmd.PersistentFlags().StringVarP(&repo, "repo", "r", "", "Github repo")
	rootCmd.PersistentFlags().StringVarP(&label, "label", "l", "", "Release-note label")
	rootCmd.PersistentFlags().StringVarP(&outputFile, "output", "o", "./release-note", "Path to output file")
	rootCmd.PersistentFlags().StringVarP(&token, "token", "t", "", "Github token file (optional)")
	rootCmd.PersistentFlags().StringVarP(&previousRelease, "previous_release", "p", "", "Previous release")
	rootCmd.PersistentFlags().StringVarP(&currentRelease, "current_release", "c", "", "Current release")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func createQueryString(repo string) ([]string, error) {
	var queries []string

	startTime, err := getReleaseTime(repo, previousRelease)
	if err != nil {
		log.Printf("Failed to get created time of previous release -- %s: %s", previousRelease, err)
		return nil, err
	}

	if currentRelease == "" {
		if currentRelease, err = gh.GetLatestRelease(repo); err != nil {
			log.Printf("Failed to get latest release version when current_release is missing: %s", err)
			return nil, err
		}
		log.Printf("Last release version: %s", currentRelease)
	}
	endTime, err := getReleaseTime(repo, currentRelease)
	if err != nil {
		log.Printf("Failed to get created time of current release -- %s: %s", currentRelease, err)
		return nil, err
	}

	queries = addQuery(queries, "repo", org, "/", repo)
	queries = addQuery(queries, "label", label)
	queries = addQuery(queries, "is", "merged")
	queries = addQuery(queries, "type", "pr")
	queries = addQuery(queries, "merged", startTime, "..", endTime)
	queries = addQuery(queries, "base", "master")

	return queries, nil
}

func addQuery(queries []string, queryParts ...string) []string {
	if len(queryParts) < 2 {
		log.Printf("Not enough to form a query: %v", queryParts)
		return queries
	}
	for _, part := range queryParts {
		if part == "" {
			return queries
		}
	}

	return append(queries, fmt.Sprintf("%s:%s", queryParts[0], strings.Join(queryParts[1:], "")))
}

func getReleaseTime(repo, release string) (string, error) {
	time, err := getReleaseTagCreationTime(repo, release)
	if err != nil {
		log.Println("Failed to get created time of this release tag")
		return "", err
	}
	t := time.UTC()
	timeString := fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02dZ",
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second())
	return timeString, nil
}

func getReleaseTagCreationTime(repo, tag string) (createTime time.Time, err error) {
	createTime, err = gh.GetReleaseTagCreationTime(repo, tag)
	if err != nil {
		log.Printf("Cannot get the creation time of %s/%s", repo, tag)
		return time.Time{}, err
	}
	return createTime, nil
}

func groupedLabelContent(issuesResult *github.IssuesSearchResult) string {
	prGrouper := []PR{}
	existentLabels := make([]string, 3)

	for _, issue := range issuesResult.Issues {
		prGrouper = append(
			prGrouper, PR{
				Title: *issue.Title,
				Link:  *issue.URL,
				Type:  fetchLabel(issue.Labels),
			},
		)
	}
	sort.Sort(ByLabel(prGrouper))

	content := fmt.Sprintf("%s: %s -- %s\n", repo, currentRelease, previousRelease)
	for _, issue := range prGrouper {
		if !ContainsString(existentLabels, issue.Type) {
			content += fmt.Sprintf("\n## %s\n", strings.Title(issue.Type))
			existentLabels = append(existentLabels, issue.Type)
		}
		content += fmt.Sprintf("* %s - %s\n", issue.Title, issue.Link)
	}
	return content
}
