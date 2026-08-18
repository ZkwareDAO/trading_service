package main

import (
	"fmt"

	"trading-service/internal/order"
)

type CompositeUserOrderRuntimeFactory struct {
	factories map[string]UserOrderRuntimeFactory
}

func NewCompositeUserOrderRuntimeFactory(factories map[string]UserOrderRuntimeFactory) *CompositeUserOrderRuntimeFactory {
	return &CompositeUserOrderRuntimeFactory{factories: factories}
}

func (f *CompositeUserOrderRuntimeFactory) NewUserOrderRuntime(user *order.User) (UserOrderRuntime, error) {
	factory := f.factories[user.Exchange]
	if factory == nil {
		return nil, fmt.Errorf("unsupported user order runtime exchange: %s", user.Exchange)
	}
	return factory.NewUserOrderRuntime(user)
}
