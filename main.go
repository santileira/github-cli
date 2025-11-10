package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

type PR struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	Draft          bool   `json:"draft"`
	HTMLURL        string `json:"html_url"`
	Mergeable      *bool  `json:"mergeable"`
	MergeableState string `json:"mergeable_state"` // "clean", "blocked", "dirty", "unstable", etc.
	User           struct{ Login string `json:"login"` } `json:"user"`
	Head           struct{ SHA string `json:"sha"` }     `json:"head"`
}

func getToken() string {
	t := os.Getenv("GH_TOKEN")
	if t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func getJSON(url, token string, target any) error {
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("GitHub API error: %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func colorState(s string) string {
	sl := strings.ToLower(s)
	switch sl {
	case "success", "approved", "open":
		return color.HiGreenString(s)
	case "failure", "changes_requested", "closed":
		return color.HiRedString(s)
	case "cancelled", "skipped", "neutral", "requested", "in_progress", "queued", "action_required", "timed_out":
		return color.YellowString(s)
	default:
		return s
	}
}

// OSC 8 hyperlink helper (clickable link in supporting terminals)
func link(text, url string) string {
	if url == "" {
		return text
	}
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, text)
}

// ---- Merge-ready evaluation helpers ----

func latestReviewSummary(repo, prNum, token string) (approved bool, changesRequested bool, err error) {
	var reviews []struct {
		User  struct{ Login string `json:"login"` } `json:"user"`
		State string `json:"state"`
		// submitted_at not needed; API returns in order, but we’ll collapse by user
	}
	if err = getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s/reviews", repo, prNum), token, &reviews); err != nil {
		return
	}
	latest := map[string]string{}
	for _, r := range reviews {
		latest[r.User.Login] = strings.ToUpper(r.State) // last one seen wins
	}
	for _, st := range latest {
		switch st {
		case "APPROVED":
			approved = true
		case "CHANGES_REQUESTED":
			changesRequested = true
		}
	}
	// Also count requested reviewers (pending review)
	var reqRev struct {
		Users []struct{ Login string `json:"login"` } `json:"users"`
	}
	_ = getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s/requested_reviewers", repo, prNum), token, &reqRev)
	// pending reviewers don't block merge-ready if branch rules don't require them,
	// but usually they do. We *don’t* block on pending here; you can change this if needed.
	return
}

func checksAllGreen(repo, sha, token string) (green bool, err error) {
	var checks struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`     // queued, in_progress, completed
			Conclusion string `json:"conclusion"` // success, failure, cancelled, skipped, neutral, timed_out, action_required
		} `json:"check_runs"`
	}
	if err = getJSON(fmt.Sprintf("https://api.github.com/repos/%s/commits/%s/check-runs", repo, sha), token, &checks); err != nil {
		return
	}
	for _, c := range checks.CheckRuns {
		if strings.ToLower(c.Status) != "completed" {
			return false, nil
		}
		switch strings.ToLower(c.Conclusion) {
		case "success", "neutral", "skipped":
			// ok
		default:
			// failure, cancelled, timed_out, action_required, etc.
			return false, nil
		}
	}
	return true, nil
}

func canMergeNow(pr PR, reviewsApproved bool, reviewsChangesRequested bool, checksGreen bool) bool {
	if strings.ToLower(pr.State) != "open" {
		return false
	}
	if pr.Mergeable == nil || !*pr.Mergeable {
		// Some repos set mergeable late; rely primarily on mergeable_state
	}
	if strings.ToLower(pr.MergeableState) != "clean" {
		return false
	}
	if reviewsChangesRequested {
		return false
	}
	if !reviewsApproved {
		return false
	}
	if !checksGreen {
		return false
	}
	return true
}

// ---- Presentation ----

func prStatus(repo, prNum, author, token string) error {
	if prNum != "" {
		var pr PR
		if err := getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s", repo, prNum), token, &pr); err != nil {
			return err
		}

		// Make the PR number clickable
		fmt.Printf("%s %s (%s)\n", link(fmt.Sprintf("#%d", pr.Number), pr.HTMLURL), pr.Title, colorState(pr.State))
		fmt.Printf("Author: %s\n", pr.User.Login)

		// --- Reviewers (submitted + requested) ---
		var reviews []struct {
			User  struct{ Login string `json:"login"` } `json:"user"`
			State string `json:"state"`
		}
		if err := getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s/reviews", repo, prNum), token, &reviews); err == nil {
			fmt.Println("Reviewers:")
			seen := map[string]string{}
			for _, r := range reviews {
				seen[r.User.Login] = r.State
			}
			var reqRev struct {
				Users []struct{ Login string `json:"login"` } `json:"users"`
				Teams []struct{ Name string `json:"name"` }   `json:"teams"`
			}
			_ = getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s/requested_reviewers", repo, prNum), token, &reqRev)
			for _, u := range reqRev.Users {
				seen[u.Login] = "requested"
			}
			for name, st := range seen {
				fmt.Printf("  - %s (%s)\n", name, colorState(st))
			}
			for _, t := range reqRev.Teams {
				fmt.Printf("  - Team: %s (%s)\n", t.Name, color.YellowString("requested"))
			}
		}

		// --- GitHub Actions (Checks) ---
		var checks struct {
			CheckRuns []struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
			} `json:"check_runs"`
		}

		if err := getJSON(fmt.Sprintf("https://api.github.com/repos/%s/commits/%s/check-runs", repo, pr.Head.SHA), token, &checks); err == nil {
			type row struct{ Name, Status, URL string }
			rows := make([]row, 0, len(checks.CheckRuns))

			for _, c := range checks.CheckRuns {
				st := c.Conclusion
				if st == "" {
					st = c.Status
				}
				rows = append(rows, row{Name: c.Name, Status: st, URL: c.HTMLURL})
			}

			// Sort by priority: failures first, then skipped/neutral/in_progress, success last
			rank := func(s string) int {
				s = strings.ToLower(s)
				switch s {
				case "failure", "timed_out", "action_required":
					return 0
				case "cancelled", "skipped", "neutral", "in_progress", "queued":
					return 1
				case "success":
					return 2
				default:
					return 1
				}
			}
			sort.Slice(rows, func(i, j int) bool {
				ri, rj := rank(rows[i].Status), rank(rows[j].Status)
				if ri != rj {
					return ri < rj
				}
				return rows[i].Name < rows[j].Name
			})

			fmt.Println("GitHub Actions:")
			for _, r := range rows {
				clickableName := link(r.Name, r.URL)
				fmt.Printf("  - %s: %s\n", clickableName, colorState(r.Status))
			}
		}
		return nil
	}

	// --- List PRs by author ---
	if author == "" {
		return fmt.Errorf("need --pr or --author")
	}

	var data struct {
		Items []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			State   string `json:"state"`
			HTMLURL string `json:"html_url"`
		} `json:"items"`
	}
	q := fmt.Sprintf("repo:%s+is:pr+author:%s", repo, author)
	url := fmt.Sprintf("https://api.github.com/search/issues?q=%s", strings.ReplaceAll(q, "+", "%20"))
	if err := getJSON(url, token, &data); err != nil {
		return err
	}
	for _, it := range data.Items {
		fmt.Printf("%s %s (%s)\n", link(fmt.Sprintf("#%d", it.Number), it.HTMLURL), it.Title, colorState(it.State))
	}
	return nil
}

// ---- Notifications ----

func notifyMac(title, message string) {
	_ = exec.Command("osascript", "-e",
		fmt.Sprintf(`display notification "%s" with title "%s"`, escapeOSA(message), escapeOSA(title)),
	).Run()
}

func escapeOSA(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func notifyITerm(message string) {
	// OSC 9 notification (iTerm banner)
	fmt.Printf("\033]9;%s\007", message)
}

func main() {
	var repo, prNum, author string
	var watch bool

	root := &cobra.Command{
		Use:   "ghprs <repo>",
		Short: "Show PR status by number or author",
		Args:  cobra.MinimumNArgs(1),
	}
	cmd := &cobra.Command{Use: "status", Short: "Show status"}
	cmd.Flags().StringVar(&repo, "repo", "", "owner/repo (overrides positional)")
	cmd.Flags().StringVar(&prNum, "pr", "", "PR number")
	cmd.Flags().StringVar(&author, "author", "", "author login")
	cmd.Flags().BoolVar(&watch, "watch", false, "refresh every minute (and notify when merge-ready)")

	cmd.RunE = func(_ *cobra.Command, args []string) error {
		if repo == "" {
			repo = args[0]
		}
		token := getToken()
		if token == "" {
			return fmt.Errorf("missing GH_TOKEN and no gh auth token available")
		}

		// Track last "ready to merge" state to avoid spamming
		lastReady := false

		// Channel for user commands when in watch mode
		userCmd := make(chan string, 1)
		if watch && prNum != "" {
			go func() {
				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					userCmd <- strings.TrimSpace(scanner.Text())
				}
			}()
		}

		for {
			fmt.Print("\033[H\033[2J") // clear screen

			// Fetch PR fresh for readiness detection
			if prNum != "" {
				var pr PR
				if err := getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s", repo, prNum), token, &pr); err == nil {
					approved, changesReq, _ := latestReviewSummary(repo, prNum, token)
					green, _ := checksAllGreen(repo, pr.Head.SHA, token)
					ready := canMergeNow(pr, approved, changesReq, green)

					// Show full status
					_ = prStatus(repo, prNum, author, token)

					if ready && !lastReady {
						msg := fmt.Sprintf("PR #%d is READY to merge ✅", pr.Number)
						notifyMac("GitHub PR", msg)
						notifyITerm(msg)
					}
					lastReady = ready

					if watch {
						fmt.Println()
						if pr.Draft {
							fmt.Println(color.YellowString("📝 PR is in DRAFT mode"))
							fmt.Println(color.HiCyanString("Type 'ready' to mark it as ready for review"))
							fmt.Println(color.HiCyanString("Type 'merge' to attempt merge anyway"))
						} else if ready {
							fmt.Println(color.HiGreenString("🎉 PR is READY to merge!"))
							fmt.Println(color.HiCyanString("Type 'merge' to merge now"))
						} else {
							fmt.Println(color.YellowString("⏳ Waiting for PR to be ready..."))
							fmt.Println(color.HiCyanString("Type 'merge' to attempt merge anyway"))
						}
					}
				} else {
					fmt.Println("error:", err)
				}
			} else {
				// Author listing path
				_ = prStatus(repo, prNum, author, token)
			}

			if !watch {
				break
			}

			fmt.Println(time.Now().Format("15:04:05"), "⏳ refreshing in 1m...")

			// Wait for either timeout or user command
			select {
			case cmd := <-userCmd:
				if strings.ToLower(cmd) == "merge" {
					// Fetch latest PR state
					var pr PR
					if err := getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s", repo, prNum), token, &pr); err != nil {
						fmt.Println(color.HiRedString("❌ Error fetching PR: %v", err))
						time.Sleep(3 * time.Second)
						continue
					}

					approved, changesReq, _ := latestReviewSummary(repo, prNum, token)
					green, _ := checksAllGreen(repo, pr.Head.SHA, token)
					ready := canMergeNow(pr, approved, changesReq, green)

					if !ready {
						fmt.Println(color.HiRedString("\n❌ PR is NOT ready to merge:"))
						if strings.ToLower(pr.State) != "open" {
							fmt.Printf("  • PR is %s (must be open)\n", pr.State)
						}
						if strings.ToLower(pr.MergeableState) != "clean" {
							fmt.Printf("  • Mergeable state: %s (must be clean)\n", pr.MergeableState)
						}
						if changesReq {
							fmt.Println("  • Changes requested by reviewers")
						}
						if !approved {
							fmt.Println("  • Missing required approvals")
						}
						if !green {
							fmt.Println("  • Checks are not all passing")
						}
						fmt.Println("\nPress Enter to continue watching...")
						time.Sleep(5 * time.Second)
						continue
					}

					// Execute merge with squash
					fmt.Println(color.HiGreenString("\n✅ Merging PR #%d with squash...", pr.Number))
					mergeCmd := exec.Command("gh", "pr", "merge", prNum, "--repo", repo, "--squash", "--auto", "--delete-branch")
					mergeCmd.Stdout = os.Stdout
					mergeCmd.Stderr = os.Stderr
					if err := mergeCmd.Run(); err != nil {
						fmt.Println(color.HiRedString("❌ Merge failed: %v", err))
						time.Sleep(3 * time.Second)
						continue
					}

					fmt.Println(color.HiGreenString("✅ Squash merge completed successfully!"))
					return nil
				} else if strings.ToLower(cmd) == "ready" {
					// Fetch latest PR state
					var pr PR
					if err := getJSON(fmt.Sprintf("https://api.github.com/repos/%s/pulls/%s", repo, prNum), token, &pr); err != nil {
						fmt.Println(color.HiRedString("❌ Error fetching PR: %v", err))
						time.Sleep(3 * time.Second)
						continue
					}

					// Check if PR is a draft
					if !pr.Draft {
						fmt.Println(color.HiYellowString("\n⚠️  PR is already ready for review (not a draft)"))
						time.Sleep(3 * time.Second)
						continue
					}

					// Execute ready command
					fmt.Println(color.HiGreenString("\n✅ Marking PR #%d as ready for review...", pr.Number))
					readyCmd := exec.Command("gh", "pr", "ready", prNum, "--repo", repo)
					readyCmd.Stdout = os.Stdout
					readyCmd.Stderr = os.Stderr
					if err := readyCmd.Run(); err != nil {
						fmt.Println(color.HiRedString("❌ Failed to mark PR as ready: %v", err))
						time.Sleep(3 * time.Second)
						continue
					}

					fmt.Println(color.HiGreenString("✅ PR is now ready for review!"))
					time.Sleep(2 * time.Second)
				}
			case <-time.After(time.Minute):
				// Continue to next iteration
			}
		}
		return nil
	}

	root.AddCommand(cmd)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
