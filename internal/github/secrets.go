package github

import (
	"context"
	"net/url"
)

func (c *Client) variableURL(repo, name string) string {
	return c.repoURL(repo, "/actions/variables/"+url.PathEscape(name))
}

func (c *Client) secretURL(repo, name string) string {
	return c.repoURL(repo, "/actions/secrets/"+url.PathEscape(name))
}

// GetVariable reads a repository's Actions variable. A 404 is not found:
// GitHub answers it both for a missing variable and for a repository the
// token cannot see.
func (c *Client) GetVariable(ctx context.Context, repo, name string) (value string, found bool, err error) {
	var v struct {
		Value string `json:"value"`
	}
	err = c.getJSON(ctx, c.variableURL(repo, name), &v)
	if IsKind(err, ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v.Value, true, nil
}

// DeleteVariable deletes a repository's Actions variable. A variable already
// gone (404) is not deleted and not an error.
func (c *Client) DeleteVariable(ctx context.Context, repo, name string) (deleted bool, err error) {
	return c.deleteItem(ctx, c.variableURL(repo, name))
}

// DeleteSecret deletes a repository's Actions secret, as DeleteVariable
// deletes a variable.
func (c *Client) DeleteSecret(ctx context.Context, repo, name string) (deleted bool, err error) {
	return c.deleteItem(ctx, c.secretURL(repo, name))
}
