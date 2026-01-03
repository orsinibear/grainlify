package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Repo struct {
	ID    int64 `json:"id"`
	Owner struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"owner"`
	FullName        string `json:"full_name"`
	Private         bool   `json:"private"`
	StargazersCount int    `json:"stargazers_count"`
	ForksCount      int    `json:"forks_count"`
	Description     string `json:"description"`
	Permissions struct {
		Admin bool `json:"admin"`
		Push  bool `json:"push"`
		Pull  bool `json:"pull"`
	} `json:"permissions"`
}

func (c *Client) GetRepo(ctx context.Context, accessToken string, fullName string) (Repo, error) {
	// fullName is owner/repo.
	owner, repo, err := splitFullName(fullName)
	if err != nil {
		return Repo{}, err
	}
	u := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Repo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Repo{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Repo{}, fmt.Errorf("github repo fetch failed: status %d", resp.StatusCode)
	}

	var r Repo
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Repo{}, err
	}
	if r.ID == 0 || r.FullName == "" {
		return Repo{}, fmt.Errorf("invalid github repo response")
	}
	return r, nil
}

func splitFullName(fullName string) (string, string, error) {
	s := strings.TrimSpace(fullName)
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo full name (expected owner/repo)")
	}
	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("invalid repo full name (expected owner/repo)")
	}
	return owner, repo, nil
}


