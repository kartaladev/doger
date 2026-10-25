package services

import (
	"github.com/kartaladev/doger"
)

// ServicesPackage is the global package for business logic services.
var ServicesPackage = doger.Package(
	doger.Lazy(func(i doger.Injector) (*UserService, error) {
		return &UserService{
			DB:     doger.MustInvokeAs[Database](i),
			Cache:  doger.MustInvokeAs[Cache](i),
			Logger: doger.MustInvokeAs[Logger](i),
		}, nil
	}),
	doger.Lazy(func(i doger.Injector) (*OrderService, error) {
		return &OrderService{
			DB:     doger.MustInvokeAs[Database](i),
			Cache:  doger.MustInvokeAs[Cache](i),
			Logger: doger.MustInvokeAs[Logger](i),
		}, nil
	}),
)
