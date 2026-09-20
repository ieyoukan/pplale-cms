package ghapp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// TokenProvider mints the credential used for repository calls.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
}

// Client performs the repository operations pplale-cms needs against a single
// repository. It deliberately exposes no push-to-default-branch operation:
// every change reaches PPLALE-web through a reviewed pull request.
type Client struct {
	Owner      string
	Repo       string
	BaseBranch string
	API        *API
	Tokens     TokenProvider
}

// File is one file written by a pull request.
type File struct {
	Path    string
	Content []byte
}

// PullRequestInput describes the branch, commit and pull request to create.
type PullRequestInput struct {
	Branch        string
	Title         string
	Body          string
	CommitMessage string
	Files         []File
}

// PullRequest is the created pull request.
type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

// FileContent reads a file from the base branch.
func (c *Client) FileContent(ctx context.Context, path string) ([]byte, error) {
	var out struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	endpoint := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s",
		c.Owner, c.Repo, encodePath(path), url.QueryEscape(c.BaseBranch))
	if err := c.get(ctx, endpoint, &out); err != nil {
		return nil, err
	}
	if out.Encoding != "base64" {
		return nil, fmt.Errorf("ghapp: 想定外のエンコーディング %q (%s)", out.Encoding, path)
	}
	// The contents API wraps base64 at 60 columns.
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("ghapp: %s のデコードに失敗しました: %w", path, err)
	}
	return decoded, nil
}

// CreatePullRequest writes every file in a single commit on a new branch and
// opens a pull request against the base branch.
func (c *Client) CreatePullRequest(ctx context.Context, in PullRequestInput) (PullRequest, error) {
	if in.Branch == "" || in.Branch == c.BaseBranch {
		return PullRequest{}, errors.New("ghapp: ベースブランチへの直接コミットは禁止です")
	}
	if len(in.Files) == 0 {
		return PullRequest{}, errors.New("ghapp: 変更ファイルがありません")
	}

	var baseRef struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	refPath := fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s", c.Owner, c.Repo, encodePath(c.BaseBranch))
	if err := c.get(ctx, refPath, &baseRef); err != nil {
		return PullRequest{}, err
	}

	var baseCommit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	commitPath := fmt.Sprintf("/repos/%s/%s/git/commits/%s", c.Owner, c.Repo, baseRef.Object.SHA)
	if err := c.get(ctx, commitPath, &baseCommit); err != nil {
		return PullRequest{}, err
	}

	type treeEntry struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	entries := make([]treeEntry, 0, len(in.Files))
	for _, f := range in.Files {
		var blob struct {
			SHA string `json:"sha"`
		}
		body := map[string]string{
			"content":  base64.StdEncoding.EncodeToString(f.Content),
			"encoding": "base64",
		}
		if err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/git/blobs", c.Owner, c.Repo), body, &blob); err != nil {
			return PullRequest{}, err
		}
		entries = append(entries, treeEntry{Path: f.Path, Mode: "100644", Type: "blob", SHA: blob.SHA})
	}

	var tree struct {
		SHA string `json:"sha"`
	}
	treeBody := map[string]any{"base_tree": baseCommit.Tree.SHA, "tree": entries}
	if err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/git/trees", c.Owner, c.Repo), treeBody, &tree); err != nil {
		return PullRequest{}, err
	}

	var commit struct {
		SHA string `json:"sha"`
	}
	commitBody := map[string]any{
		"message": in.CommitMessage,
		"tree":    tree.SHA,
		"parents": []string{baseRef.Object.SHA},
	}
	if err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/git/commits", c.Owner, c.Repo), commitBody, &commit); err != nil {
		return PullRequest{}, err
	}

	refBody := map[string]string{"ref": "refs/heads/" + in.Branch, "sha": commit.SHA}
	if err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/git/refs", c.Owner, c.Repo), refBody, nil); err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) && apiErr.StatusCode == 422 {
			return PullRequest{}, fmt.Errorf("ghapp: ブランチ %q は既に存在します: %w", in.Branch, err)
		}
		return PullRequest{}, err
	}

	var pr PullRequest
	prBody := map[string]any{
		"title": in.Title,
		"head":  in.Branch,
		"base":  c.BaseBranch,
		"body":  in.Body,
	}
	if err := c.post(ctx, fmt.Sprintf("/repos/%s/%s/pulls", c.Owner, c.Repo), prBody, &pr); err != nil {
		return PullRequest{}, err
	}
	return pr, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.call(ctx, "GET", path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.call(ctx, "POST", path, body, out)
}

func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return err
	}
	return c.API.do(ctx, method, path, "token "+token, body, out)
}

func encodePath(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}
