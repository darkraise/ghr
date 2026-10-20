package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"

	"golang.org/x/crypto/nacl/box"
)

// RepoPublicKey is the key GitHub seals a repository's Actions secrets with.
type RepoPublicKey struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

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

// SetVariable updates a repository's Actions variable, or creates it when
// the update finds none.
func (c *Client) SetVariable(ctx context.Context, repo, name, value string) error {
	body := map[string]string{"name": name, "value": value}
	_, err := c.mutate(ctx, http.MethodPatch, c.variableURL(repo, name), body)
	if IsKind(err, ErrNotFound) {
		_, err = c.mutate(ctx, http.MethodPost, c.repoURL(repo, "/actions/variables"), body)
	}
	return err
}

// DeleteVariable deletes a repository's Actions variable. A variable already
// gone (404) is not deleted and not an error.
func (c *Client) DeleteVariable(ctx context.Context, repo, name string) (deleted bool, err error) {
	return c.deleteItem(ctx, c.variableURL(repo, name))
}

func (c *Client) RepoPublicKey(ctx context.Context, repo string) (RepoPublicKey, error) {
	var k RepoPublicKey
	err := c.getJSON(ctx, c.repoURL(repo, "/actions/secrets/public-key"), &k)
	return k, err
}

// SecretExists reports whether a repository has the Actions secret; GitHub
// never returns a secret's value.
func (c *Client) SecretExists(ctx context.Context, repo, name string) (bool, error) {
	var s struct {
		Name string `json:"name"`
	}
	err := c.getJSON(ctx, c.secretURL(repo, name), &s)
	if IsKind(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// SetSecret creates or replaces a repository's Actions secret, sealed with
// the repository's public key.
func (c *Client) SetSecret(ctx context.Context, repo, name, value string) error {
	k, err := c.RepoPublicKey(ctx, repo)
	if err != nil {
		return err
	}
	sealed, err := SealSecret(k.Key, value)
	if err != nil {
		return err
	}
	_, err = c.mutate(ctx, http.MethodPut, c.secretURL(repo, name), map[string]string{"encrypted_value": sealed, "key_id": k.KeyID})
	return err
}

// DeleteSecret deletes a repository's Actions secret, as DeleteVariable
// deletes a variable.
func (c *Client) DeleteSecret(ctx context.Context, repo, name string) (deleted bool, err error) {
	return c.deleteItem(ctx, c.secretURL(repo, name))
}

// SealSecret encrypts value for GitHub with an anonymous sealed box to the
// base64 Curve25519 public key, and returns it base64-encoded.
func SealSecret(publicKeyB64, value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(raw) != 32 {
		return "", errors.New("github: the repository's public key is not a base64 32-byte key")
	}
	var pk [32]byte
	copy(pk[:], raw)
	sealed, err := box.SealAnonymous(nil, []byte(value), &pk, rand.Reader)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}
