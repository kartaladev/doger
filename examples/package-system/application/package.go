package application

import (
	"github.com/kartaladev/doger"
)

// ApplicationPackage is the global package for the main application.
var ApplicationPackage = doger.Package(
	doger.Lazy(func(i doger.Injector) (*Application, error) {
		return &Application{
			Config:       doger.MustInvokeAs[Configuration](i),
			UserService:  doger.MustInvokeAs[UserService](i),
			OrderService: doger.MustInvokeAs[OrderService](i),
			Logger:       doger.MustInvokeAs[Logger](i),
		}, nil
	}),
)
