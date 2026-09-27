package attribution

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotMerged is returned by ParseGitHub when the webhook event is
// not pull_request.closed with merged=true. Callers swallow it as
// a "nothing to do" outcome.
var ErrNotMerged = errors.New("attribution: pull_request not merged")

// ParseGitHub turns a raw `pull_request` webhook body into the
// provider-agnostic MergedEvent. Returns ErrNotMerged for non-merge
// events so the caller can 202-ack without re-parsing.
//
// Schema reference: https://docs.github.com/en/webhooks/webhook-events-and-payloads#pull_request
func ParseGitHub(body []byte) (MergedEvent, error) {
	var p struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number   int       `json:"number"`
			Merged   bool      `json:"merged"`
			MergedAt time.Time `json:"merged_at"`
			HTMLURL  string    `json:"html_url"`
		} `json:"pull_request"`
		Repository struct {
			Name  string `json:"name"`
			Owner struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return MergedEvent{}, fmt.Errorf("attribution: parse: %w", err)
	}
	if p.Action != "closed" || !p.PullRequest.Merged {
		return MergedEvent{}, ErrNotMerged
	}
	if p.Repository.Owner.Login == "" || p.Repository.Name == "" || p.PullRequest.Number == 0 {
		return MergedEvent{}, errors.New("attribution: missing repo/pr fields")
	}
	return MergedEvent{
		Provider:       "github",
		InstallationID: p.Installation.ID,
		RepoOwner:      p.Repository.Owner.Login,
		RepoName:       p.Repository.Name,
		PRNumber:       p.PullRequest.Number,
		MergedAt:       p.PullRequest.MergedAt,
	}, nil
}
