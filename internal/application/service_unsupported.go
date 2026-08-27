//go:build !darwin

package application

import (
	"context"
	"errors"
)

type unsupportedService struct{}

func NewService() Service {
	return &unsupportedService{}
}

func (*unsupportedService) List(context.Context) ([]Application, error) {
	return nil, errors.New("this platform is not supported yet")
}

func (*unsupportedService) Launch(context.Context, string) error {
	return errors.New("this platform is not supported yet")
}

func (*unsupportedService) Quit(context.Context, string) error {
	return errors.New("this platform is not supported yet")
}
