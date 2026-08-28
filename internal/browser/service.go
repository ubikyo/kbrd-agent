package browser

import "context"

type Browser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"-"`
}

type Service interface {
	List(context.Context) ([]Browser, error)
	Open(ctx context.Context, id string, url string) error
}
