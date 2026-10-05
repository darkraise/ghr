package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func TestAvailableReposKeepsTheOwnersSorted(t *testing.T) {
	b, m, gh := newBackend(t)
	gh.userRepos = []github.UserRepo{
		{Name: "zeta", Private: true, Owner: github.Account{Login: "darkraise"}},
		{Name: "DarkCloud", Private: true, Owner: github.Account{Login: "DarkRaise"}},
		{Name: "elsewhere", Private: true, Owner: github.Account{Login: "someone"}},
		{Name: "booklore", Private: false, Owner: github.Account{Login: "darkraise"}},
	}
	got, err := b.AvailableRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []model.AvailableRepo{
		{Name: "booklore", Private: false},
		{Name: "DarkCloud", Private: true, Configured: true},
		{Name: "zeta", Private: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	gh.userRepos = nil
	if got, err := b.AvailableRepos(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no repos: %#v %v", got, err)
	}
	gh.repoErr = &github.APIError{Status: 502, Kind: github.ErrServer}
	if _, err := b.AvailableRepos(context.Background()); !errors.Is(err, gh.repoErr) {
		t.Fatalf("GitHub error: %v", err)
	}
	m.degraded = "GitHub rejected the token"
	if _, err := b.AvailableRepos(context.Background()); apiStatus(err) != 503 {
		t.Fatalf("degraded: %v", err)
	}
}
