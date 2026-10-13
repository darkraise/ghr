package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The storage SKUs, normalized. The usage endpoint spells them "Actions
// storage" and "Packages storage".
const (
	SKUActionsStorage  = "actions_storage"
	SKUPackagesStorage = "packages_storage"
)

// NormalizeSKU lower-cases s, turns spaces into underscores and drops
// hyphens, so a SKU or a unit compares equal however GitHub spells it.
func NormalizeSKU(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(s), " ", "_"), "-", "")
}

func storageSKU(sku string) bool { return sku == SKUActionsStorage || sku == SKUPackagesStorage }

// Cache is one Actions cache entry.
type Cache struct {
	ID             int64     `json:"id"`
	Key            string    `json:"key"`
	Ref            string    `json:"ref"`
	SizeInBytes    int64     `json:"size_in_bytes"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// UsageItem is one day's storage of one SKU in one repository. Repo is
// empty for Packages.
type UsageItem struct {
	Date     time.Time
	SKU      string
	Repo     string
	Quantity float64
}

type Retention struct {
	Days    int `json:"days"`
	MaxDays int `json:"maximum_allowed_days"`
}

// IsTooManyPages reports whether a listing failed for being longer than
// 50 pages.
func IsTooManyPages(err error) bool { return errors.Is(err, errTooManyPages) }

// OwnerType reports whether the owner is an organization.
func (c *Client) OwnerType(ctx context.Context) (isOrg bool, err error) {
	var u struct {
		Type string `json:"type"`
	}
	if err := c.getJSON(ctx, c.BaseURL+"/users/"+url.PathEscape(c.Owner), &u); err != nil {
		return false, err
	}
	return u.Type == "Organization", nil
}

// TokenLogin is the login of the token's user.
func (c *Client) TokenLogin(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := c.getJSON(ctx, c.BaseURL+"/user", &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

func (c *Client) billingURL(isOrg bool, year int, month time.Month) string {
	kind := "users"
	if isOrg {
		kind = "organizations"
	}
	return fmt.Sprintf("%s/%s/%s/settings/billing/usage?year=%d&month=%d", c.BaseURL, kind, url.PathEscape(c.Owner), year, int(month))
}

// StorageUsage lists the month's daily Actions and Packages storage in
// GB-hours. GitHub's schema says quantity is an integer; it is fractional.
func (c *Client) StorageUsage(ctx context.Context, isOrg bool, year int, month time.Month) ([]UsageItem, error) {
	var body struct {
		UsageItems []struct {
			Date           time.Time `json:"date"`
			SKU            string    `json:"sku"`
			Quantity       float64   `json:"quantity"`
			UnitType       string    `json:"unitType"`
			RepositoryName string    `json:"repositoryName"`
		} `json:"usageItems"`
	}
	if err := c.getJSON(ctx, c.billingURL(isOrg, year, month), &body); err != nil {
		return nil, err
	}
	var out []UsageItem
	for _, it := range body.UsageItems {
		sku := NormalizeSKU(it.SKU)
		if NormalizeSKU(it.UnitType) != "gigabytehours" || !storageSKU(sku) {
			continue
		}
		out = append(out, UsageItem{Date: it.Date, SKU: sku, Repo: it.RepositoryName, Quantity: it.Quantity})
	}
	return out, nil
}

// ListRepoArtifacts lists every artifact of the repository, expired ones
// included.
func (c *Client) ListRepoArtifacts(ctx context.Context, repo string) ([]Artifact, error) {
	var out []Artifact
	err := c.getAll(ctx, c.repoURL(repo, "/actions/artifacts?per_page=100"), func(b []byte) error {
		var page struct {
			Artifacts []Artifact `json:"artifacts"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Artifacts...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListCaches(ctx context.Context, repo string) ([]Cache, error) {
	var out []Cache
	err := c.getAll(ctx, c.repoURL(repo, "/actions/caches?per_page=100"), func(b []byte) error {
		var page struct {
			Caches []Cache `json:"actions_caches"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Caches...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteArtifact deletes one artifact. An artifact already gone (404) is
// not deleted and not an error.
func (c *Client) DeleteArtifact(ctx context.Context, repo string, id int64) (deleted bool, err error) {
	return c.deleteItem(ctx, c.repoURL(repo, fmt.Sprintf("/actions/artifacts/%d", id)))
}

// DeleteCache deletes one cache entry, as DeleteArtifact does an artifact.
func (c *Client) DeleteCache(ctx context.Context, repo string, id int64) (deleted bool, err error) {
	return c.deleteItem(ctx, c.repoURL(repo, fmt.Sprintf("/actions/caches/%d", id)))
}

func (c *Client) deleteItem(ctx context.Context, u string) (bool, error) {
	_, err := c.mutate(ctx, http.MethodDelete, u, nil)
	if IsKind(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) Retention(ctx context.Context, repo string) (Retention, error) {
	var r Retention
	err := c.getJSON(ctx, c.repoURL(repo, "/actions/permissions/artifact-and-log-retention"), &r)
	return r, err
}

func (c *Client) SetRetention(ctx context.Context, repo string, days int) error {
	_, err := c.mutate(ctx, http.MethodPut, c.repoURL(repo, "/actions/permissions/artifact-and-log-retention"), map[string]int{"days": days})
	return err
}
