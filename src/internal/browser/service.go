package browser

import "context"

type Browser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Whether this is the browser the machine itself opens a link with,
	// so an editor can offer that answer instead of asking for one the
	// system already has.
	Default bool   `json:"isDefault"`
	Path    string `json:"-"`
}

type Service interface {
	List(context.Context) ([]Browser, error)
	Open(ctx context.Context, id string, url string) error
}
