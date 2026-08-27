package application

import "context"

type Application struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CanQuit bool   `json:"canQuit"`
	Path    string `json:"-"`
}

type Service interface {
	List(context.Context) ([]Application, error)
	Launch(context.Context, string) error
	Quit(context.Context, string) error
}
