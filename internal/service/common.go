// Package service holds business logic: rules, transactions and audit.
// Services depend on repository interfaces and never on HTTP types.
package service

func ptr[T any](v T) *T { return &v }
