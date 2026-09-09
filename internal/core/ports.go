package core

import (
	"context"
	"errors"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("idempotency conflict")
)

type Store interface {
	ReserveCreate(context.Context, string, string, []byte) (*ProviderPayment, error)
	CompleteCreate(context.Context, string, ProviderPayment, []byte) error
	FailCreate(context.Context, string, string) error
	FindPayment(context.Context, string, string) (ProviderPayment, error)
	FindPaymentByReference(context.Context, string, string) (ProviderPayment, error)
	UpdateStatus(context.Context, string, string, string, string) error
	RecordProviderEvent(context.Context, ProviderEvent, []byte) (bool, error)
	ReserveRefund(context.Context, string, string, []byte, CreateRefundCommand) (*ProviderRefund, error)
	CompleteRefund(context.Context, string, ProviderRefund, []byte) error
	FailRefund(context.Context, string, string) error
	FindRefund(context.Context, string, string) (ProviderRefund, error)
}
