package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	githubTokenURL   = "https://github.com/login/oauth/access_token"
	githubUserURL    = "https://api.github.com/user"
	githubOrgsURL    = "https://api.github.com/user/orgs"
	httpClientTimout = 10 * time.Second
)

// exchangeGitHubCode exchanges an OAuth code for an access token.
func (s *service) exchangeGitHubCode(ctx context.Context, code string) (string, error) {
	data := url.Values{}
	data.Set("client_id", s.cfg.Auth.GitHub.ClientID)
	data.Set("client_secret", s.cfg.Auth.GitHub.ClientSecret)
	data.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: httpClientTimout}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("making request: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if tokenResp.Error != "" {
		return "", fmt.Errorf("github oauth error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("no access token in response")
	}

	return tokenResp.AccessToken, nil
}

// getGitHubUser gets the authenticated user's profile from GitHub.
func (s *service) getGitHubUser(ctx context.Context, accessToken string) (*GitHubUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: httpClientTimout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var userResp struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}

	if err := json.Unmarshal(body, &userResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &GitHubUser{
		ID:    strconv.FormatInt(userResp.ID, 10),
		Login: userResp.Login,
	}, nil
}

// getGitHubUserOrgs gets the organizations the user belongs to.
func (s *service) getGitHubUserOrgs(ctx context.Context, accessToken string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubOrgsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: httpClientTimout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("making request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var orgsResp []struct {
		Login string `json:"login"`
	}

	if err := json.Unmarshal(body, &orgsResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	orgs := make([]string, 0, len(orgsResp))
	for _, org := range orgsResp {
		orgs = append(orgs, org.Login)
	}

	return orgs, nil
}
