//go:build !darwin

package browser

import (
	"context"
	"errors"
)

type unsupportedService struct{}

func NewService() Service {
	return &unsupportedService{}
}

func (*unsupportedService) List(context.Context) ([]Browser, error) {
	return nil, errors.New("this platform is not supported yet")
}

func (*unsupportedService) Open(context.Context, string, string) error {
	return errors.New("this platform is not supported yet")
}
